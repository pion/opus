/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Standalone observation of pinned libopus floating-point PLC primitives. */
#include "config.h"
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "celt/mathops.h"
#include "opus_custom.h"
#include "celt/bands.h"
#include "celt/celt_lpc.h"
#include "celt/modes.h"
#include "celt/pitch.h"

static void array(const float *v, int n) {
    printf("[");
    for (int i=0; i<n; i++) printf("%s%.9g", i ? "," : "", v[i]);
    printf("]");
}
static void array_bits(const float *v, int n, int stride) {
    printf("[");
    for (int i=0; i<n; i++) {
        uint32_t bits; memcpy(&bits,&v[i*stride],sizeof(bits));
        printf("%s%u",i ? "," : "",bits);
    }
    printf("]");
}
int main(void) {
#ifdef PLC_ANTI_COLLAPSE
    int anti_err;
    OpusCustomMode *anti_mode=opus_custom_mode_create(48000,960,&anti_err);
    if(!anti_mode||anti_err) return 1;
    float x[240]={0}, loge[42]={0}, prev1[42], prev2[42];
    unsigned char masks[21]={0};
    int pulses[21]={0};
    for(int i=0;i<42;i++) prev1[i]=prev2[i]=-28.f;
    anti_collapse(anti_mode,x,masks,1,1,240,0,1,loge,prev1,prev2,pulses,1,0,0);
    uint32_t bits[2]; memcpy(bits,x,sizeof(bits));
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"bits\":[%u,%u]}\n",bits[0],bits[1]);
    return 0;
#endif
#ifdef PLC_EXP2
    printf("[");
    for(int i=-816;i<=512;i++) {
        float input=i/16.f, output=celt_exp2(input);
        uint32_t bits; memcpy(&bits,&output,sizeof(bits));
        printf("%s%u",i==-816?"":",",bits);
    }
    printf("]\n"); return 0;
#endif
#ifdef PLC_MDCT_TABLES
    int mdct_err;
    OpusCustomMode *mdct_mode=opus_custom_mode_create(48000,960,&mdct_err);
    if(!mdct_mode||mdct_err) return 1;
    const mdct_lookup *mdct=&mdct_mode->mdct;
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"plans\":[");
    for(int shift=3;shift>=0;shift--) {
        int transform=mdct->n>>shift, frame=transform>>1, nfft=transform>>2;
        int offset=0;
        for(int prior=0;prior<shift;prior++) offset+=mdct->n>>(prior+1);
        const kiss_fft_state *fft=mdct->kfft[shift];
        int twiddle_stride=1<<(fft->shift>0?fft->shift:0);
        printf("%s{\"frame\":%d,\"trig\":",shift==3?"":",",frame);
        array_bits(mdct->trig+offset,frame,1);
        printf(",\"twiddle_r\":"); array_bits(&fft->twiddles[0].r,nfft,2*twiddle_stride);
        printf(",\"twiddle_i\":"); array_bits(&fft->twiddles[0].i,nfft,2*twiddle_stride);
        printf(",\"bitrev\":[");
        for(int i=0;i<nfft;i++) printf("%s%d",i?",":"",fft->bitrev[i]);
        printf("],\"factors\":[");
        for(int i=0;fft->factors[2*i];i++) printf("%s%d,%d",i?",":"",fft->factors[2*i],fft->factors[2*i+1]);
        printf("]}");
    }
    printf("]}\n"); return 0;
#endif
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
