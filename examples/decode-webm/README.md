# decode-webm
decode-webm demonstrates decoding a webm file and saving the results to a single file

## Instructions
### Install decode-webm
Download and install the decode-webm

```
go install github.com/pion/opus/examples/decode-webm@latest
```

### Create a webm file to decode
Encode Opus into an webm file, or use one that you already have.

```
ffmpeg -i $INPUT_FILE -c:a libopus -ac 1 -b:a 64K output.webm
```

### Decode
Demux and decode the provided `webm` file. The output audio samples will be saved to disk.

```
decode-webm `pwd`/output.webm `audio-samples.pcm`
```

### Play your audio
Now play the audio with the tool of your choice.

```
gst-launch-1.0 filesrc location=audio-samples.pcm ! audio/x-raw, format=S16LE, rate=48000,channels=1  ! autoaudiosink -v
```

```
ffplay -f s16le -ar 48000 audio-samples.pcm
```
