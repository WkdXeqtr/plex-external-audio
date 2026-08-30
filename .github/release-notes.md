Install by running the `.exe`. One UAC prompt and it is done.

The program needs `ffprobe`, part of ffmpeg, to read your audio files.
If your machine has none, the installer downloads one and keeps just that single file; if the download cannot be made it says so and carries on, and you can point it at an ffmpeg of your own afterwards.

The build is unsigned, so SmartScreen will call it an unknown publisher.
If you want to be sure of what you downloaded, compare the checksum below against `Get-FileHash <file> -Algorithm SHA256`: the installer was built by GitHub Actions from the source at this tag, and the run is public.

See the [README](https://github.com/WkdXeqtr/plex-external-audio#readme) for what it does and how.
