#!/usr/bin/env bash
# PLACEHOLDER: code signing is intentionally NOT done yet. build.sh calls this for every binary.
# When the app goes public, fill in the section for each OS (keep secrets in the environment or a
# keychain, never in the repo):
#
#   macOS    codesign --force --options runtime --timestamp -s "Developer ID Application: NAME (TEAMID)" "$1"
#            then notarize: xcrun notarytool submit <zip> --keychain-profile jarvis --wait ; xcrun stapler staple
#            (a bare binary cannot be stapled: ship it inside a signed .pkg or .dmg)
#   Windows  signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /a "$1"
#            (or `osslsigncode` / Azure Trusted Signing from Linux)
#   Linux    nothing required; optionally `gpg --detach-sign dist/SHA256SUMS`
#
# Until then this only says so.
echo "    (unsigned: $2 binary $1)"
