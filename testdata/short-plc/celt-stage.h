/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Observation-only access to the pinned CELT decoder state. The library copy
 * of celt_decoder.c is not linked for this translation unit. */
#include <stdint.h>
#include <string.h>
#include "celt/celt_decoder.c"

static void celt_stage_float_bits(const float *values, int count) {
    fprintf(stderr, "[");
    for (int i=0; i<count; i++) {
        uint32_t bits;
        memcpy(&bits, &values[i], sizeof(bits));
        fprintf(stderr, "%s%u", i ? "," : "", bits);
    }
    fprintf(stderr, "]");
}

static void emit_celt_stage(const OpusDecoder *decoder, int step, int first, int last) {
    int offset;
    memcpy(&offset, decoder, sizeof(offset));
    const CELTDecoder *c = (const CELTDecoder *)((const char *)decoder + offset);
    const int stride = DECODE_BUFFER_SIZE + c->overlap;
    const float *old_band_e = c->_decode_mem + stride*c->channels;
    const float *lpc = old_band_e + 8*c->mode->nbEBands;
    float ac[CELT_LPC_ORDER+1];
    float corrected[CELT_LPC_ORDER+1];
    float recomputed_lpc[CELT_LPC_ORDER];
    _celt_autocorr(c->_decode_mem + DECODE_BUFFER_SIZE-MAX_PERIOD, ac,
        c->mode->window, c->overlap, CELT_LPC_ORDER, MAX_PERIOD, c->arch);
    memcpy(corrected, ac, sizeof(ac));
    corrected[0] *= 1.0001f;
    for (int i=1; i<=CELT_LPC_ORDER; i++)
        corrected[i] -= corrected[i]*(0.008f*0.008f)*i*i;
    _celt_lpc(recomputed_lpc, corrected, CELT_LPC_ORDER);
    fprintf(stderr, "%s{\"step\":%d,\"pitch\":%d,\"lpc\":",
        step == first ? "[" : ",", step, c->last_pitch_index);
    celt_stage_float_bits(lpc, CELT_LPC_ORDER*c->channels);
    fprintf(stderr, ",\"ac\":");
    celt_stage_float_bits(ac, CELT_LPC_ORDER+1);
    fprintf(stderr, ",\"corrected\":");
    celt_stage_float_bits(corrected, CELT_LPC_ORDER+1);
    fprintf(stderr, ",\"recomputed_lpc\":");
    celt_stage_float_bits(recomputed_lpc, CELT_LPC_ORDER);
    fprintf(stderr, ",\"history\":");
    celt_stage_float_bits(c->_decode_mem, stride*c->channels);
    fprintf(stderr, "}%s", step == last ? "]\n" : "");
}
