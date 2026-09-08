/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include "opus.h"
#ifdef PLC_TRACE
#include "trace.h"
#endif
static void check(int result) { if (result<0) { fprintf(stderr,"%s\n",opus_strerror(result)); exit(1); } }
static unsigned rng;
static int signal_sample(int kind, int i, int ch) {
    int period = 120+ch*61;
    int v = ((i%period)-period/2)*70;
    if (kind==1) v = ((i%(i<5000?period:period+29))-(period+29)/2)*70;
    if (kind==2) v = (int)(v*exp(-i/7000.0));
    if (kind==3) { rng=1664525*rng+1013904223; v=(int)(rng>>19)-4096; }
    if (kind==4) v = i%997==0?15000:0;
    if (kind==5) v = 0;
    return v;
}
static void run(int kind,int channels,int output_channels,int rate,int mode,int sequence) {
    int error,offset=0;
    rng=42;
    OpusEncoder *enc=opus_encoder_create(48000,channels,mode?OPUS_APPLICATION_AUDIO:OPUS_APPLICATION_RESTRICTED_LOWDELAY,&error); check(error);
    OpusDecoder *dec=opus_decoder_create(rate,output_channels,&error); check(error);
    /* Pion retains signaled intensity-stereo phase inversion. libopus 1.6.1
     * disables it by default for mono output; match Pion's existing policy
     * explicitly so the PLC comparison starts from the same signal. */
    check(opus_decoder_ctl(dec, OPUS_SET_PHASE_INVERSION_DISABLED(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_BITRATE(64000*channels)));
    check(opus_encoder_ctl(enc, OPUS_SET_VBR(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_DTX(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_COMPLEXITY(10)));
    if(mode) {
        check(opus_encoder_ctl(enc,11002,mode==1?1000:mode==2?1001:1002));
        check(opus_encoder_ctl(enc,OPUS_SET_BANDWIDTH(mode==1?OPUS_BANDWIDTH_WIDEBAND:OPUS_BANDWIDTH_FULLBAND)));
    }
    printf("{\"signal\":%d,\"channels\":%d,\"output_channels\":%d,\"rate\":%d,\"mode\":%d,\"sequence\":%d,\"steps\":[",kind,channels,output_channels,rate,mode,sequence);
    for(int step=0;step<22;step++) {
        int lost=sequence!=2 && ((step>=4&&step<=10)||step==12||step==15);
        int sizes[]={120,240,480,960};
        int samples=lost&&sequence==1&&mode==0?sizes[(step-4)%4]:960;
        if(sequence==3) samples=sizes[step%4];
        if(sequence==4&&channels==2) {
            if(step==2) check(opus_encoder_ctl(enc,OPUS_SET_FORCE_CHANNELS(1)));
            if(step==11) check(opus_encoder_ctl(enc,OPUS_SET_FORCE_CHANNELS(2)));
        }
        if(step==3&&mode==1) check(opus_encoder_ctl(enc,11002,1002));
        if(step==3&&mode==3) {
            check(opus_encoder_ctl(enc,11002,1000));
            check(opus_encoder_ctl(enc,OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH_WIDEBAND)));
        }
        opus_int16 input[1920],out[1920]; unsigned char packet[1275];
        for(int i=0;i<samples;i++) for(int ch=0;ch<channels;ch++) input[i*channels+ch]=(opus_int16)signal_sample(kind,offset+i,ch);
        offset+=samples;
        int bytes=opus_encode(enc,input,samples,packet,sizeof(packet)); check(bytes);
        int n=opus_decode(dec,lost?NULL:packet,lost?0:bytes,out,samples*rate/48000,0); check(n);
#ifdef PLC_TRACE
        trace_plc(dec);
#endif
        opus_uint32 range; check(opus_decoder_ctl(dec,OPUS_GET_FINAL_RANGE(&range)));
        printf("%s{\"packet\":\"",step?",":"");
        if(!lost) for(int i=0;i<bytes;i++) printf("%02x",packet[i]);
        printf("\",\"samples\":%d,\"range\":%u,\"pcm\":[",n,range);
        for(int i=0;i<n*output_channels;i++) printf("%s%d",i?",":"",out[i]);
        printf("]}");
    }
    printf("]}"); opus_encoder_destroy(enc); opus_decoder_destroy(dec);
}
int main(void) {
    int rates[]={8000,12000,16000,24000,48000},first=1;
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
    for(int kind=0;kind<6;kind++) for(int ch=1;ch<=2;ch++) for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) for(int seq=0;seq<4;seq++) {
        if(!first) printf(","); first=0;
        run(kind,ch,oc,rates[r],0,seq);
    }
    for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) {
        printf(","); run(0,2,oc,rates[r],0,4);
    }
    for(int mode=1;mode<=3;mode++) for(int ch=1;ch<=2;ch++) for(int oc=1;oc<=2;oc++) {
        printf(","); run(0,ch,oc,16000,mode,0);
    }
    printf("]}\n"); return 0;
}
