# decoder
decoder demonstrates decoding a ogg file and saving the results to a single file

## Instructions
### Install decoder
Download and install the decoder

```
go install github.com/pion/opus/examples/decoder@latest
```

### Create a ogg file to decode
Encode Opus into an ogg file, or use one that you already have. This implementation doesn't
support most Opus features yet, so encoding will be constrained.

```
ffmpeg -i $INPUT_FILE -c:a libopus -ac 1 -b:a 10K output.ogg
```

### Decode
Demux and decode the provided `ogg` file. The output audio samples will be saved to disk.

```
decoder `pwd`/output.ogg `audio-samples.pcm`
```

### Play your audio
Now play the audio with the tool of your choice.

```
gst-launch-1.0 filesrc location=pion-out ! audio/x-raw, format=F32LE, rate=16000,channels=1  ! autoaudiosink -v
```

```
ffplay -f f32le -ar 16000
```
