Install by running the `.exe`. One UAC prompt and it is done.

**ffmpeg has to be installed already** - the program needs `ffprobe` to read your audio files.
If it cannot find one, the installer says so and points at the setting to fill in.

The build is unsigned, so SmartScreen will call it an unknown publisher.
If you want to be sure of what you downloaded, compare the checksum below against `Get-FileHash <file> -Algorithm SHA256`: the installer was built by GitHub Actions from the source at this tag, and the run is public.

See the [README](https://github.com/WkdXeqtr/plex-external-audio#readme) for what it does and how.
