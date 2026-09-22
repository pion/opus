/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Observe the first decoder spectrum without modifying the reference source.
 * Resolve the real external function before remapping calls in celt_decoder.c. */
#include "config.h"
#define CELT_DECODER_C
#include "celt/bands.h"
#include "celt/modes.h"
#ifdef PLC_FIRST_SPECTRUM
static int spectrum_capture=1;
#else
static int spectrum_capture;
#endif
static int spectrum_observed;
static int mdct_observed;
static int synthesis_observed;
static float observed_mdct_frequency[960];
static void spectrum_bits(const char *name,const float *values,int count) {
    fprintf(stderr,"\"%s\":[",name);
    for(int i=0;i<count;i++) {
        uint32_t bits; memcpy(&bits,&values[i],sizeof(bits));
        fprintf(stderr,"%s%u",i?",":"",bits);
    }
    fprintf(stderr,"]");
}
static void observe_denormalise(const CELTMode *m,const celt_norm *x,celt_sig *freq,
    const celt_glog *energy,int start,int end,int mult,int downsample,int silence) {
    denormalise_bands(m,x,freq,energy,start,end,mult,downsample,silence);
    if(spectrum_capture&&!spectrum_observed++) {
        fprintf(stderr,"{");
        spectrum_bits("normalized",x,mult*m->eBands[end]);
        fprintf(stderr,","); spectrum_bits("frequency",freq,mult*m->shortMdctSize);
        fprintf(stderr,","); spectrum_bits("energy",energy,m->nbEBands);
    }
}
#define denormalise_bands observe_denormalise
#include "celt/celt.h"
static void observe_mdct_backward(const mdct_lookup *l, kiss_fft_scalar *in, celt_sig *out,
    const opus_val16 *window,int overlap,int shift,int stride,int arch) {
    if(spectrum_capture) {
        int block=mdct_observed++;
        int input_count=(l->n>>shift)>>1;
        for(int i=0;i<input_count;i++) observed_mdct_frequency[block+i*stride]=in[i*stride];
    }
    clt_mdct_backward_c(l,in,out,window,overlap,shift,stride,arch);
}
#undef clt_mdct_backward
#define clt_mdct_backward observe_mdct_backward
static void observe_comb_filter(opus_val32 *output,opus_val32 *input,int period0,int period1,int count,
    opus_val16 gain0,opus_val16 gain1,int tapset0,int tapset1,const celt_coef *window,int overlap,int arch) {
    if(spectrum_capture&&spectrum_observed&&!synthesis_observed++) {
        fprintf(stderr,",");
        spectrum_bits("synthesis",input,960);
    }
    comb_filter(output,input,period0,period1,count,gain0,gain1,tapset0,tapset1,window,overlap,arch);
}
#define comb_filter observe_comb_filter
#include "celt/celt_decoder.c"
#undef denormalise_bands
#undef clt_mdct_backward
#undef comb_filter
static void observe_postfilter(const OpusDecoder *decoder) {
    if(!spectrum_capture) return;
    int offset;
    memcpy(&offset,decoder,sizeof(offset));
    const CELTDecoder *c=(const CELTDecoder *)((const char *)decoder+offset);
    const float *postfilter=c->_decode_mem+DECODE_BUFFER_SIZE-960;
    fprintf(stderr,","); spectrum_bits("mdct_frequency",observed_mdct_frequency,960);
    fprintf(stderr,","); spectrum_bits("postfilter",postfilter,960);
    fprintf(stderr,"}\n");
    spectrum_capture=0;
}
