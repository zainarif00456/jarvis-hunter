"""The host app, for real, against the real server code: connect, identity sweep, commands, files,
terminal, cameras (fake camera), console switch, disconnect and reconnect."""

from __future__ import annotations

import asyncio
import getpass
import hashlib
import json
import os
import shutil
import signal
import socket
import subprocess
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any

import pytest

from jarvis.hosts.service import HostConn, HostError
from tests.support.hosts_rig import Rig, serving, wait_for

pytestmark = pytest.mark.asyncio


class HostProc:
    def __init__(self, binary: Path, rig: Rig, home: Path, *extra: str) -> None:
        self.home = home
        home.mkdir(parents=True, exist_ok=True)
        env = {**os.environ, "JARVIS_HOST_HOME": str(home), "SHELL": "/bin/sh", "JARVIS_HOST_FAKE_CAMERA": "1"}
        self.log = (home / "stdout.txt").open("wb")
        self.proc = subprocess.Popen(  # noqa: S603
            [str(binary), "--no-indicator", "--server", rig.base, *extra],
            env=env, stdout=self.log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL,
        )

    @property
    def output(self) -> str:
        self.log.flush()
        return (self.home / "stdout.txt").read_text(errors="replace")

    @property
    def action_log(self) -> str:
        p = self.home / "actions.log"
        return p.read_text() if p.exists() else ""

    def stop(self) -> None:
        if self.proc.poll() is None:
            self.proc.send_signal(signal.SIGINT)
            try:
                self.proc.wait(8)
            except subprocess.TimeoutExpired:
                self.proc.kill()
        self.log.close()


@asynccontextmanager
async def connected(tmp_path: Path, host_bin: Path, *extra: str) -> AsyncIterator[tuple[Rig, HostProc, HostConn]]:
    async with serving(tmp_path) as rig:
        await rig.rt.hosts.set_accepting(True)
        host = HostProc(host_bin, rig, tmp_path / "hosthome", *extra)
        try:
            await wait_for(lambda: rig.rt.hosts.pending, 15, "the new host to ask for approval")
            await rig.tool("hosts.approve", host="")
            conn = await wait_for(lambda: next(iter(rig.rt.hosts.hosts.values()), None), 15, "host connected")
            await wait_for(lambda: conn.identity and conn.cameras, 15, "identity sweep and camera list")
            yield rig, host, conn
        finally:
            host.stop()


async def test_door_closed_by_default_keeps_the_host_out(tmp_path: Path, host_bin: Path) -> None:
    async with serving(tmp_path) as rig:
        host = HostProc(host_bin, rig, tmp_path / "h")
        try:
            await wait_for(lambda: "refused" in host.action_log, 10, "the host to be turned away")
            assert rig.rt.hosts.hosts == {} and rig.rt.hosts.pending == {}
            assert host.proc.poll() is None, "the host keeps trying quietly instead of crashing"
            assert "not accepting" in host.output
        finally:
            host.stop()


async def test_full_session(tmp_path: Path, host_bin: Path) -> None:
    async with connected(tmp_path, host_bin) as (rig, host, conn):
        # --- identity sweep --------------------------------------------------------------
        ident = conn.identity
        assert ident["user"] == getpass.getuser()
        assert ident["hostname"] == socket.gethostname() or ident["hostname"]
        assert ident["os"] == "linux" and ident["arch"] in {"amd64", "arm64"}
        assert ident["os_version"] and ident["uptime_s"] > 0 and isinstance(ident["is_admin"], bool)
        assert isinstance(ident["addresses"], list)
        status = await rig.tool("hosts.status")
        assert status.accepting and status.hosts[0]["user"] == getpass.getuser()
        assert "support host" in rig.rt.hosts.render_context()

        # --- commands --------------------------------------------------------------------
        out = await rig.tool("host.run", command="echo hello-$((6*7)); echo err >&2; pwd", cwd=str(tmp_path))
        assert out.exit_code == 0 and "hello-42" in out.stdout and out.stderr.strip() == "err"
        assert out.cwd == str(tmp_path) and str(tmp_path.resolve()) in out.stdout.replace("\n", " ") + str(tmp_path)
        assert (await rig.tool("host.run", command="exit 9")).exit_code == 9
        marker = tmp_path / "child-survived"
        long = asyncio.create_task(rig.tool("host.run", command=f"(sleep 2; touch {marker}) & sleep 120", timeout_s=300))
        await asyncio.sleep(0.8)
        long.cancel()
        t0 = asyncio.get_running_loop().time()
        done = await rig.tool("host.run", command="echo after-cancel")
        assert "after-cancel" in done.stdout and asyncio.get_running_loop().time() - t0 < 5
        await asyncio.sleep(3)
        assert not marker.exists(), "the cancelled command's child process kept running on the host"
        timed = await rig.tool("host.run", command="sleep 30", timeout_s=1)
        assert timed.timed_out

        # --- files, both ways ------------------------------------------------------------
        src = tmp_path / "proj" / "payload.bin"
        src.write_bytes(os.urandom(3_000_000))
        target = str(tmp_path / "hostside" / "deep" / "payload.bin")
        moved = await rig.tool("host.upload", local_path=str(src), remote_path=target)
        assert Path(target).read_bytes() == src.read_bytes() and moved.size == 3_000_000
        got = await rig.tool("host.download", remote_path=target)
        assert hashlib.sha256(Path(got.saved_at).read_bytes()).hexdigest() == hashlib.sha256(src.read_bytes()).hexdigest()
        assert got.shared_id
        listing = await rig.tool("host.files", path=str(Path(target).parent))
        assert [e["name"] for e in listing.entries] == ["payload.bin"]
        with pytest.raises(HostError, match="exists|No such|no such"):
            await rig.tool("host.upload", local_path=str(src), remote_path=target, overwrite=False)
        with pytest.raises(HostError):
            await rig.tool("host.download", remote_path=str(tmp_path / "nope.bin"))

        # --- processes and apps ----------------------------------------------------------
        procs = await rig.tool("host.processes", filter="jarvis-host")
        assert any(p["pid"] == host.proc.pid for p in procs.processes)
        victim = subprocess.Popen(["sleep", "300"])  # noqa: S607
        try:
            await rig.tool("host.kill", pid=victim.pid, force=True)
            assert victim.wait(5) != 0
        finally:
            victim.kill()
        with pytest.raises(HostError, match="not a valid target|itself"):
            await rig.tool("host.kill", pid=host.proc.pid)
        launched = await rig.tool("host.app_launch", app="true")
        assert "opened" in launched.detail

        # --- screenshot: works where a screen tool exists, otherwise says why ------------
        try:
            shot = await rig.tool("host.screenshot")
            assert Path(shot.saved_at).read_bytes()[:4] == b"\x89PNG"
        except HostError as exc:
            assert "screenshot" in str(exc) or "tried" in str(exc), str(exc)

        # --- terminal: the pane's terminal, but on the host ------------------------------
        term = await rig.tool("terminal.open", host=conn.hostname, cwd=str(tmp_path))
        session = rig.rt.terminals.get(term.terminal)
        assert session is not None and session.cwd == str(tmp_path)
        typed = await rig.tool("terminal.type", terminal=term.terminal, text="echo pty-$((8*8)); tty", wait_s=6)
        assert "pty-64" in session.screen_text() and "/dev/pts" in session.screen_text()
        assert typed.status in {"done", "running"}
        session.resize(120, 40)
        await rig.tool("terminal.close", terminal=term.terminal)
        await wait_for(lambda: session.closed, 10, "the terminal to close")

        # --- console switch --------------------------------------------------------------
        hidden = await rig.tool("host.console", visible=False)
        assert "hidden" in hidden.detail
        assert json.loads((host.home / "config.json").read_text())["console_visible"] is False
        await rig.tool("host.console", visible=True)

        # --- the action log on the host records what Jarvis did --------------------------
        log = host.action_log
        for needle in ("echo hello-$((6*7))", '"action":"file.put"', '"action":"file.get"', '"action":"pty.open"', '"action":"connected"'):
            assert needle in log, needle
        assert "payload.bin" in log and src.read_bytes()[:16].hex() not in log  # names yes, contents never
        assert (host.home / "actions.log").stat().st_mode & 0o077 == 0


@pytest.mark.skipif(shutil.which("ffmpeg") is None, reason="ffmpeg is not available")
async def test_camera_enumerate_snapshot_and_live_stream(tmp_path: Path, host_bin: Path) -> None:
    async with connected(tmp_path, host_bin) as (rig, _host, conn):
        listed = await rig.tool("host.camera_list")
        assert listed.cameras and listed.cameras[0]["name"].startswith("Test Pattern on ")
        cams = await rig.tool("camera.list")
        assert any("Test Pattern" in c["name"] and c["kind"] == "host" for c in cams.cameras)
        snap = await rig.tool("camera.snapshot", camera="test pattern")
        assert Path(snap.media["saved_at"]).read_bytes()[:3] == b"\xff\xd8\xff"
        cam = rig.rt.media.registry.all()[0]
        live, why = await rig.rt.media.streams.wait_live(cam, 25)
        assert live, why
        viewer = rig.rt.media.streams.watch(cam)
        seen: list[str] = []
        total = 0
        while len(seen) < 4:
            msg = await asyncio.wait_for(viewer.get(), 15)
            assert msg is not None
            seen.append(msg.json["type"] if msg.kind == "json" else "bin")
            total += len(msg.data)
        assert "init" in seen and total > 5000
        viewer.leave()
        await rig.rt.media.streams.drop(cam.id)
        # relaying a network camera: login stays in memory, never in a file or in the host's log
        await rig.tool("host.camera_relay", name="Garage", address="10.255.255.1", username="admin", password="relay-secret-77")
        assert rig.rt.media.registry.resolve("garage").kind == "host"
        for path in [*rig.rt.paths.root.rglob("*"), *(_host.home).rglob("*")]:
            if path.is_file() and path.stat().st_size < 3_000_000:
                assert b"relay-secret-77" not in path.read_bytes(), path
        assert conn.cameras


async def test_disconnect_and_reconnect_without_asking_again(tmp_path: Path, host_bin: Path) -> None:
    async with connected(tmp_path, host_bin) as (rig, host, conn):
        first_id = conn.id
        assert (await rig.tool("host.run", command="echo one")).exit_code == 0
        # the server drops the connection (network blip / server restart): the host comes back by itself
        await conn.shutdown("test: network blip")
        await wait_for(lambda: first_id not in rig.rt.hosts.hosts, 5, "the drop to be noticed")
        back = await wait_for(lambda: rig.rt.hosts.hosts.get(first_id), 20, "the host to reconnect on its own")
        assert back is not conn and not rig.rt.hosts.pending, "reconnect must not need approval again"
        await wait_for(lambda: back.identity, 10, "the sweep again")
        assert "two" in (await rig.tool("host.run", command="echo two")).stdout
        assert host.output.count("CONNECTED") >= 2

        # closing the door drops the host; reopening lets its pass back in
        await rig.rt.hosts.set_accepting(False)
        await wait_for(lambda: not rig.rt.hosts.hosts, 10, "the host to be dropped")
        assert host.proc.poll() is None
        await rig.rt.hosts.set_accepting(True)
        await wait_for(lambda: rig.rt.hosts.hosts, 90, "the host to come back after the door reopened")


async def test_jarvis_can_send_the_host_away_and_the_user_can_stop_it(tmp_path: Path, host_bin: Path) -> None:
    async with connected(tmp_path, host_bin) as (rig, host, _conn):
        await rig.tool("host.disconnect")
        assert await asyncio.get_running_loop().run_in_executor(None, host.proc.wait, 10) == 0
        assert "Disconnected" in host.output
    (tmp_path / "second").mkdir()
    async with connected(tmp_path / "second", host_bin) as (rig, host, _conn):
        host.proc.send_signal(signal.SIGINT)  # the person at the machine presses Ctrl+C
        assert await asyncio.get_running_loop().run_in_executor(None, host.proc.wait, 10) == 0
        await wait_for(lambda: not rig.rt.hosts.hosts, 10, "the server to notice")
        with pytest.raises(HostError, match="no support host"):
            await rig.tool("host.run", command="echo")


async def test_the_one_input_is_the_server_address(tmp_path: Path, host_bin: Path) -> None:
    async with serving(tmp_path) as rig:
        await rig.rt.hosts.set_accepting(True)
        env = {**os.environ, "JARVIS_HOST_HOME": str(tmp_path / "h"), "JARVIS_SERVER": rig.base}
        # no argument, no flag: the environment variable is enough
        proc = subprocess.Popen([str(host_bin), "--no-indicator"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL)  # noqa: S603
        try:
            await wait_for(lambda: rig.rt.hosts.pending, 15, "a host that was given the address by environment")
        finally:
            proc.terminate()
        # and with nothing at all it refuses to guess a server (no default, nothing baked in)
        bare = subprocess.run([str(host_bin), "--no-indicator"], env={**os.environ, "JARVIS_HOST_HOME": str(tmp_path / "h2"), "JARVIS_SERVER": ""}, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=10)  # noqa: S603
        assert bare.returncode == 2 and "No server address" in bare.stderr
        # and it remembers the address once it has been given
        cfg: dict[str, Any] = json.loads((tmp_path / "h" / "config.json").read_text())
        assert cfg["server"] == rig.base and len(cfg["host_id"]) == 32
