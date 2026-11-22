JARVIS SUPPORT HOST
===================
Lets Jarvis (Zain's assistant) look after THIS computer. Only run it on a computer whose owner
agrees. It connects OUT to Jarvis; it opens nothing on this computer's network.

1. Open the folder for this computer: mac / windows / linux.
2. Double-click the Start file (Mac: Start-on-Mac.command, Windows: Start-on-Windows.bat,
   Linux: ./start-on-linux.sh).
3. The first time it asks ONE question: the address of Zain's Jarvis server. Type it, press Enter.
   Zain approves the computer once; after that it reconnects by itself.

While it is connected you will see a window "Jarvis is connected to this computer" with a
Disconnect button. Everything Jarvis does is written to a log file (path shown in the console).
To stop at any time: press Disconnect, or Ctrl+C in the console window, or type  stop.

Cameras need ffmpeg: it is in the "bin" folder if Zain added it, otherwise install it
(Mac: brew install ffmpeg, Windows: winget install ffmpeg, Linux: your package manager).

The app is not code-signed yet. Mac: right-click Start-on-Mac.command > Open the first time.
Windows: SmartScreen > More info > Run anyway.
