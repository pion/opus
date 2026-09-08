/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Observation-only build: compile the exact reference decoder in this TU.
 * The library's copy is not pulled from the static archive. No algorithms or
 * configuration/state are changed. OpusDecoder's first int is celt_dec_offset
 * in src/opus_decoder.c at 22244de5a79bd1d6d623c32e72bf1954b56235be. */
#include <string.h>
#include "celt/celt_decoder.c"
static void trace_plc(const OpusDecoder *decoder) {
    int offset;
    memcpy(&offset, decoder, sizeof(offset));
    const CELTDecoder *c = (const CELTDecoder *)((const char *)decoder + offset);
    const float *e = c->_decode_mem + (DECODE_BUFFER_SIZE + c->overlap) * c->channels;
    fprintf(stderr, "{\"type\":%d,\"loss\":%d,\"plc\":%d,\"skip\":%d,\"pitch\":%d,\"energy\":[",
        c->last_frame_type, c->loss_duration, c->plc_duration, c->skip_plc, c->last_pitch_index);
    for (int i=0; i<8*c->mode->nbEBands; i++) fprintf(stderr, "%s%.9g", i ? "," : "", e[i]);
    fprintf(stderr, "]}\n");
}
