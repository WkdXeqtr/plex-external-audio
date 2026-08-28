Install by running the `.exe`. One UAC prompt and it is done.

**ffmpeg has to be installed already** - the program needs `ffprobe` to read your audio files.
If it cannot find one, the installer says so and points at the setting to fill in.

The build is unsigned, so SmartScreen will call it an unknown publisher.
Check `SHA256SUMS.txt` against the file if you want to be sure of what you downloaded: it was built by GitHub Actions from the source at this tag, and the run is public.

See the [README](https://github.com/WkdXeqtr/plex-external-audio#readme) for what it does and how.
