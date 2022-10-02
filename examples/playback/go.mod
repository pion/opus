module github.com/pion/opus/examples/playback

go 1.18

require (
	github.com/faiface/beep v1.1.0
	github.com/pion/opus v0.0.0
)

require (
	github.com/hajimehoshi/oto v0.7.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/exp v0.0.0-20190306152737-a1d7652674e8 // indirect
	golang.org/x/image v0.0.0-20190227222117-0694c2d4d067 // indirect
	golang.org/x/mobile v0.0.0-20190415191353-3e0bab5405d6 // indirect
	golang.org/x/sys v0.0.0-20190626150813-e07cf5db2756 // indirect
)

replace github.com/pion/opus => ./../..
