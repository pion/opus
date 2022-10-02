# playback
playback demonstrates decoding a ogg file and then playing using the [faiface/beep](github.com/faiface/beep) library

## Instructions
### Install playback
Download and install playback

```
go install github.com/pion/opus/examples/playback@latest
```

### Create a ogg file to decode
Encode Opus into an ogg file, or use one that you already have. This implementation doesn't
support most Opus features yet, so encoding will be constrained.

```
ffmpeg -i $INPUT_FILE -c:a libopus -ac 1 -b:a 10K output.ogg
```

### Decode
Demux and decode the provided `ogg` file. The output audio samples will be played.

```
playback `pwd`/output.ogg
```
