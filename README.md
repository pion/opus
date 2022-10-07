<h1 align="center">
  <br>
  Opus
  <br>
</h1>
<h4 align="center">Pure Go implementation of the Opus Codec</h4>
<p align="center">
  <a href="https://pion.ly"><img src="https://img.shields.io/badge/pion-opus-gray.svg?longCache=true&colorB=brightgreen" alt="Opus"></a>
  <a href="https://pion.ly/slack"><img src="https://img.shields.io/badge/join-us%20on%20slack-gray.svg?longCache=true&logo=slack&colorB=brightgreen" alt="Slack Widget"></a>
  <br>
  <a href="https://pkg.go.dev/github.com/pion/opus"><img src="https://godoc.org/github.com/pion/opus?status.svg" alt="GoDoc"></a>
  <a href="https://codecov.io/gh/pion/opus"><img src="https://codecov.io/gh/pion/opus/branch/master/graph/badge.svg" alt="Coverage Status"></a>
  <a href="https://goreportcard.com/report/github.com/pion/opus"><img src="https://goreportcard.com/badge/github.com/pion/opus" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>
<br>

This package provides a Pure Go implementation of the [Opus Codec](https://opus-codec.org/)

### Why Opus?

* **open and royalty-free** - No license fees or restrictions. Use it as you wish!
* **versatile** - Wide bitrate support. Can be used in constrained networks and high quality stereo.
* **ubiquitous** - Used in video streaming, gaming, storing music and video conferencing.

### Why a Go implementation?

* **empower interesting use cases** - This project also exports the internals of the Encoder and Decoder.
                                      Allowing for things like analysis of a Opus bitstream without decoding the entire thing.
* **learning** - This project was written to be read by others. It includes excerpts and links to [RFC 6716](https://datatracker.ietf.org/doc/rfc6716/)
* **safety** - Go provides memory safety. Avoids a class of bugs that are devastating in sensitive environments.
* **maintainability** - Go was designed to build simple, reliable, and efficient software.
* **inspire** - Go is a power language, but lacking in media libraries. We hope this project inspires the next generation to build
                more media libraries for Go.

You can read more [here](https://pion.ly/blog/pion-opus/)

### Running
See our [examples](examples) for demonstrations of how to use this package.

### Get Involved!
We would love to have you involved! This project needs a lot of help before it can be useful to everyone. See the Roadmap for open issues and join us on [Slack](https://pion.ly/slack)

### Roadmap
See [Issue 9](https://github.com/pion/opus/issues/9)
