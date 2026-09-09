/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Standalone observation of pinned libopus floating-point PLC primitives. */
#include "config.h"
#include <stdint.h>
#include <stdio.h>
#include "opus_custom.h"
#include "celt/celt_lpc.h"
#include "celt/modes.h"
#include "celt/pitch.h"

static void array(const float *v, int n) {
    printf("[");
    for (int i=0; i<n; i++) printf("%s%.9g", i ? "," : "", v[i]);
    printf("]");
}
int main(void) {
    int err;
    OpusCustomMode *mode = opus_custom_mode_create(48000, 960, &err);
    if (!mode || err) return 1;
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
    for (int signal=0; signal<6; signal++) for (int channels=1; channels<=2; channels++) {
        float x[2][2048], low[1024], ac[25], corrected[25], lpc[24];
        uint32_t rng=42;
        for (int ch=0; ch<channels; ch++) for (int i=0; i<2048; i++) {
            rng = 1664525u*rng + 1013904223u;
            float v = (float)((i*109+ch*31)%997-498)/64;
            if (signal==1) v = (float)((int)(rng>>16)-32768)/4096;
            if (signal==2) v = i==777 || i==2031 ? 16 : 0;
            if (signal==3) v = 0;
            if (signal==4) v *= (float)(2048-i)/2048;
            if (signal==5) v = (i+ch*17)%240 < 120 ? 8 : -8;
            x[ch][i]=v;
        }
        float *input[2]={x[0],x[1]};
        pitch_downsample(input, low, 1024, channels, 2, 0);
        int pitch;
        pitch_search(low+360, low, 1328, 620, &pitch, 0);
        _celt_autocorr(x[0]+1024, ac, mode->window, 120, 24, 1024, 0);
        corrected[0]=ac[0]*1.0001f;
        for (int i=1; i<=24; i++) corrected[i]=ac[i]-ac[i]*(0.008f*0.008f)*(i*i);
        _celt_lpc(lpc, corrected, 24);
        printf("%s{\"signal\":%d,\"input\":[", signal || channels==2 ? "," : "", signal);
        for (int ch=0; ch<channels; ch++) { if(ch) printf(","); array(x[ch],2048); }
        printf("],\"low\":"); array(low,1024);
        printf(",\"ac\":"); array(ac,25);
        printf(",\"corrected\":"); array(corrected,25);
        printf(",\"lpc\":"); array(lpc,24);
        printf(",\"pitch\":%d}",720-pitch);
    }
    printf("]}\n");
    /* 48 kHz/960 uses the library's immutable built-in mode, not an allocation. */
    return 0;
}
