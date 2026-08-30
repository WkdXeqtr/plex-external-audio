# Privacy

The short version: this program sends nothing anywhere, and there is nothing here to opt out of.

## What it sends

Nothing.
There is no networking code in it at all - no telemetry, no analytics, no crash reporting, and no update check.
You do not have to take that on trust: no part of the source imports a networking package, and the whole thing is under a hundred kilobytes of Go that you can read.

## What it reads

Your Plex library database, to see which video files exist and to add rows describing the audio tracks it found.
The audio files themselves, through `ffprobe`, to learn their codec, channel layout, sample rate and language.
Its own settings file.

It never reads anything outside your Plex library and the folders your media sits in.

## What it writes on your machine

Rows in the Plex library database, describing external audio tracks.
Nothing else in that database is touched, and your media files are never modified.

Settings, in `%LOCALAPPDATA%\Plex External Audio`: the check interval, the chosen language, and how many tracks were found last time.

Logs, in `%ProgramData%\Plex External Audio` and in `%TEMP%`.
They contain paths of your media files and the command lines Plex builds for its transcoder, because that is what is needed to work out why something did not play.
They never contain the contents of your files, and the wrapper deliberately does not log its environment - an earlier version did, and wrote out every variable including unrelated secrets that happened to be set.

All of it stays on your machine.
Uninstalling removes the settings; the logs are left behind on purpose, since they are the only record of what went wrong if you are uninstalling because something did.

## The installer

The installer looks for Plex and for `ffprobe` on your own disk and in your own registry.

It makes exactly one network request, and only when your machine has no `ffprobe` at all: it downloads a published build of ffmpeg, takes `ffprobe.exe` out of it and throws the rest away.
The sources are [BtbN/FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds/releases) on GitHub, and [gyan.dev](https://www.gyan.dev/ffmpeg/builds/) if the first cannot be reached.
Nothing about you is sent with that request beyond what any download involves, and if it fails the installation carries on without it.

It contacts nothing else, ever.
