/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Build against the README's pinned generic libopus with -ffp-contract=off.
 * cc -O2 -ffp-contract=off -Iinclude softclip-reference.c .libs/libopus.a -lm
 * ./a.out | gzip -n > softclip-reference.json.gz
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "opus.h"

static void bits(const float *x, int n) {
    putchar('[');
    for (int i=0; i<n; i++) {
        uint32_t u;
        memcpy(&u, &x[i], sizeof(u));
        printf("%s%u", i ? "," : "", u);
    }
    putchar(']');
}

int main(void) {
    uint32_t rng=42;
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
    for (int channels=1; channels<=2; channels++) {
        float memory[2]={0,0};
        for (int frame=0; frame<128; frame++) {
            float samples[96];
            for (int i=0; i<48*channels; i++) {
                rng=1664525*rng+1013904223;
                /* Consecutive same-sign frames exercise carried clipping memory;
                 * mixed signs exercise zero crossings and the initial ramp. */
                float v=(float)(rng>>16)/16384.f;
                if (frame%4<2) v=(frame/4)%2 ? -v : v;
                else v-=2.f;
                samples[i]=v;
            }
            printf("%s{\"channels\":%d,\"input\":", channels==1&&frame==0 ? "" : ",", channels);
            bits(samples,48*channels);
            opus_pcm_soft_clip(samples,48,channels,memory);
            printf(",\"output\":"); bits(samples,48*channels);
            printf(",\"memory\":"); bits(memory,channels);
            putchar('}');
        }
    }
    puts("]}");
    return 0;
}
