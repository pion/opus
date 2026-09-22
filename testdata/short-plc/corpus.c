/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <stdint.h>
#include <string.h>
#include "opus.h"
#if defined(PLC_FIRST_SPECTRUM) || defined(PLC_DOWNMIX_SPECTRUM) || defined(PLC_RANDOM_STEREO_SPECTRUM) || defined(PLC_CELT_NOISE_STAGE)
#include "first-spectrum.h"
#endif
#ifdef PLC_TRACE
#include "trace.h"
#endif
#if defined(PLC_SILK_STAGE) || defined(PLC_SILK_RECOVERY_STAGE) || defined(PLC_SILK_STEREO_RECOVERY_STAGE)
#include "silk-stage.h"
#endif
#ifdef PLC_SILK_PLC_STATE
#include "silk-plc-stage.h"
#endif
#if defined(PLC_CELT_STAGE) || defined(PLC_CELT_MIXED_STAGE)
#include "celt-stage.h"
#endif
#ifdef PLC_CELT_PLC_MATH
#include "celt-plc-math.h"
#endif
static void check(int result) { if (result<0) { fprintf(stderr,"%s\n",opus_strerror(result)); exit(1); } }
static unsigned rng;
static unsigned char *rfc_pcm;
static size_t rfc_pcm_frames;

static void load_rfc_pcm(void) {
    const char *path=getenv("PLC_RFC8251_PCM");
    if(!path) { fprintf(stderr,"PLC_RFC8251_PCM is required\n"); exit(1); }
    FILE *file=fopen(path,"rb");
    if(!file) { perror(path); exit(1); }
    if(fseek(file,0,SEEK_END)!=0) { perror("fseek"); exit(1); }
    long size=ftell(file);
    if(size<=0||size%4!=0) { fprintf(stderr,"invalid RFC PCM size: %ld\n",size); exit(1); }
    rewind(file);
    rfc_pcm=malloc((size_t)size);
    if(!rfc_pcm||fread(rfc_pcm,1,(size_t)size,file)!=(size_t)size) {
        fprintf(stderr,"failed to read RFC PCM\n"); exit(1);
    }
    if(fclose(file)!=0) { perror("fclose"); exit(1); }
    rfc_pcm_frames=(size_t)size/4;
}
#ifdef PLC_NO_LOSS_FLOAT
static int float_case_first=1;
static void float_bits(const float *values,int count) {
    fprintf(stderr,"[");
    for(int i=0;i<count;i++) {
        uint32_t bits; memcpy(&bits,&values[i],sizeof(bits));
        fprintf(stderr,"%s%u",i?",":"",bits);
    }
    fprintf(stderr,"]");
}
#endif
static int signal_sample(int kind, int i, int ch) {
    if(kind==6) {
        size_t sample=2*((size_t)i%rfc_pcm_frames)+(size_t)ch;
        return (int16_t)((uint16_t)rfc_pcm[2*sample]<<8|rfc_pcm[2*sample+1]);
    }
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
#ifdef PLC_NO_LOSS_FLOAT
    int float_target=kind==0&&mode==0&&(
        (channels==1&&output_channels==1&&rate==16000&&sequence==2)||
        (channels==2&&output_channels==1&&rate==24000&&sequence==0));
    float_target|=kind==3&&mode==0&&channels==2&&output_channels==1&&rate==48000&&sequence==2;
    float_target|=kind==0&&mode==1&&channels==1&&output_channels==1&&rate==16000&&sequence==0;
    OpusDecoder *float_decoder=NULL;
    if(float_target) {
        float_decoder=opus_decoder_create(rate,output_channels,&error); check(error);
        check(opus_decoder_ctl(float_decoder,OPUS_SET_PHASE_INVERSION_DISABLED(0)));
        fprintf(stderr,"%s{\"signal\":%d,\"channels\":%d,\"output_channels\":%d,"
            "\"rate\":%d,\"mode\":%d,\"sequence\":%d,\"frames\":[",
            float_case_first?"":",",kind,channels,output_channels,rate,mode,sequence);
        float_case_first=0;
    }
#endif
    /* Pion retains signaled intensity-stereo phase inversion. libopus 1.6.1
     * disables it by default for mono output; match Pion's existing policy
     * explicitly so the PLC comparison starts from the same signal. */
    check(opus_decoder_ctl(dec, OPUS_SET_PHASE_INVERSION_DISABLED(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_BITRATE(64000*channels)));
    check(opus_encoder_ctl(enc, OPUS_SET_VBR(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_DTX(0)));
    check(opus_encoder_ctl(enc, OPUS_SET_COMPLEXITY(10)));
    if(mode) {
        check(opus_encoder_ctl(enc,11002,(mode==1||mode==4)?1000:mode==2?1001:1002));
        check(opus_encoder_ctl(enc,OPUS_SET_BANDWIDTH((mode==1||mode==4)?OPUS_BANDWIDTH_WIDEBAND:OPUS_BANDWIDTH_FULLBAND)));
    }
    printf("{\"signal\":%d,\"channels\":%d,\"output_channels\":%d,\"rate\":%d,\"mode\":%d,\"sequence\":%d,\"steps\":[",kind,channels,output_channels,rate,mode,sequence);
    for(int step=0;step<22;step++) {
        int lost=sequence!=2 && ((step>=4&&step<=10)||step==12||step==15);
        int sizes[]={120,240,480,960};
        int samples=lost&&sequence==1&&mode==0?sizes[(step-4)%4]:960;
        if(sequence==3) samples=sizes[step%4];
        if(sequence==5&&mode==2) {
            int hybrid_sizes[]={480,960};
            samples=lost?960:hybrid_sizes[step%2];
        }
        if(sequence==5&&mode==4) {
            int silk_sizes[]={480,960,1920,2880};
            int silk_loss_sizes[]={960,1920,2880};
            samples=lost?silk_loss_sizes[step%3]:silk_sizes[step%4];
        }
        if(sequence==4&&channels==2) {
            if(step==2) check(opus_encoder_ctl(enc,OPUS_SET_FORCE_CHANNELS(1)));
            if(step==11) check(opus_encoder_ctl(enc,OPUS_SET_FORCE_CHANNELS(2)));
        }
        if(step==3&&mode==1) check(opus_encoder_ctl(enc,11002,1002));
        if(step==3&&mode==3) {
            check(opus_encoder_ctl(enc,11002,1000));
            check(opus_encoder_ctl(enc,OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH_WIDEBAND)));
        }
        opus_int16 input[5760],out[5760]; unsigned char packet[1275];
        for(int i=0;i<samples;i++) for(int ch=0;ch<channels;ch++) input[i*channels+ch]=(opus_int16)signal_sample(kind,offset+i,ch);
        offset+=samples;
        int bytes=opus_encode(enc,input,samples,packet,sizeof(packet)); check(bytes);
#ifdef PLC_SILK_PLC_STATE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==3&&sequence==0&&step==10)
            silk_plc_state_capture=1;
#endif
#ifdef PLC_SILK_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==1&&sequence==0&&step==0)
            silk_stage_capture=1;
#endif
#ifdef PLC_SILK_RECOVERY_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==3&&sequence==0&&step==11)
            silk_stage_capture=1;
#endif
#ifdef PLC_SILK_STEREO_RECOVERY_STAGE
        if(kind==2&&channels==2&&output_channels==1&&rate==8000&&mode==4&&sequence==0&&step==11)
            silk_stage_capture=1;
#endif
#ifdef PLC_DOWNMIX_SPECTRUM
        if(kind==0&&channels==2&&output_channels==1&&rate==24000&&mode==0&&sequence==0&&step==0) {
            spectrum_capture=1; spectrum_observed=0; mdct_observed=0; synthesis_observed=0;
        }
#endif
#ifdef PLC_RANDOM_STEREO_SPECTRUM
        if(kind==3&&channels==2&&output_channels==1&&rate==48000&&mode==0&&sequence==2&&step==18) {
            spectrum_capture=1; spectrum_observed=0; mdct_observed=0; synthesis_observed=0;
        }
#endif
#ifdef PLC_CELT_NOISE_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==0&&step==9) {
            spectrum_capture=1; spectrum_observed=0; mdct_observed=0; synthesis_observed=0;
        }
#endif
#ifdef PLC_CELT_PLC_MATH
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==3&&step==15)
            celt_plc_math_capture=1;
#endif
        int n=opus_decode(dec,lost?NULL:packet,lost?0:bytes,out,samples*rate/48000,0); check(n);
#ifdef PLC_CELT_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==0&&step<=9)
            emit_celt_stage(dec,step,0,9);
#endif
#ifdef PLC_CELT_MIXED_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==3&&step>=11&&step<=15)
            emit_celt_stage(dec,step,11,15);
#endif
#ifdef PLC_SILK_PLC_STATE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==3&&sequence==0&&step==10)
            emit_silk_plc_state();
#endif
#ifdef PLC_SILK_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==1&&sequence==0&&step==0)
            emit_silk_stage();
#endif
#ifdef PLC_SILK_RECOVERY_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==16000&&mode==3&&sequence==0&&step==11)
            emit_silk_stage();
#endif
#ifdef PLC_SILK_STEREO_RECOVERY_STAGE
        if(kind==2&&channels==2&&output_channels==1&&rate==8000&&mode==4&&sequence==0&&step==11)
            emit_silk_stage();
#endif
#ifdef PLC_NO_LOSS_FLOAT
        if(float_target&&(sequence==2||step<4)) {
            float float_out[1920];
            int float_n=opus_decode_float(float_decoder,packet,bytes,float_out,samples*rate/48000,0); check(float_n);
            if(float_n!=n) exit(4);
            for(int i=0;i<n*output_channels;i++)
                if(!isfinite(float_out[i])||
                    (channels==1&&output_channels==1&&rate==16000&&fabsf(float_out[i])>=1)||
                    (channels==1&&output_channels==1&&rate==16000&&lrintf(float_out[i]*32768)!=out[i])) exit(5);
            fprintf(stderr,"%s",step?",":""); float_bits(float_out,n*output_channels);
        }
#endif
#ifdef PLC_FIRST_SPECTRUM
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==0&&step==0)
            observe_postfilter(dec);
#endif
#ifdef PLC_DOWNMIX_SPECTRUM
        if(kind==0&&channels==2&&output_channels==1&&rate==24000&&mode==0&&sequence==0&&step==0)
            observe_postfilter(dec);
#endif
#ifdef PLC_RANDOM_STEREO_SPECTRUM
        if(kind==3&&channels==2&&output_channels==1&&rate==48000&&mode==0&&sequence==2&&step==18)
            observe_postfilter(dec);
#endif
#ifdef PLC_CELT_NOISE_STAGE
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==0&&step==9)
            observe_postfilter(dec);
#endif
#ifdef PLC_FIRST_FLOAT
        /* Independent first-frame observation: no mutation of the canonical
         * decoder. This signal stays below clipping; verify that explicitly
         * before using the float API to localize the integer discrepancy. */
        if(kind==0&&channels==1&&output_channels==1&&rate==8000&&mode==0&&sequence==0&&step==0) {
            OpusDecoder *probe=opus_decoder_create(rate,output_channels,&error); check(error);
            check(opus_decoder_ctl(probe,OPUS_SET_PHASE_INVERSION_DISABLED(0)));
            float pcm[160];
            int count=opus_decode_float(probe,packet,bytes,pcm,160,0); check(count);
            if(count!=n) exit(2);
            fprintf(stderr,"{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"bits\":[");
            for(int i=0;i<count;i++) {
                uint32_t bits; memcpy(&bits,&pcm[i],sizeof(bits));
                if(!isfinite(pcm[i])||fabsf(pcm[i])>=1||lrintf(pcm[i]*32768)!=out[i]) exit(3);
                fprintf(stderr,"%s%u",i?",":"",bits);
            }
            fprintf(stderr,"]}\n");
            opus_decoder_destroy(probe);
        }
#endif
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
    printf("]}");
#ifdef PLC_NO_LOSS_FLOAT
    if(float_target) { fprintf(stderr,"]}"); opus_decoder_destroy(float_decoder); }
#endif
    opus_encoder_destroy(enc); opus_decoder_destroy(dec);
}
int main(void) {
    int rates[]={8000,12000,16000,24000,48000},first=1;
    load_rfc_pcm();
#ifdef PLC_NO_LOSS_FLOAT
    fprintf(stderr,"{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
#endif
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
    /* Pure SILK loss/recovery coverage across signal classes, API rates and
     * channel conversion. Mode 4 never transitions away from SILK. */
    for(int kind=0;kind<6;kind++) for(int ch=1;ch<=2;ch++) for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) {
        printf(","); run(kind,ch,oc,rates[r],4,0);
    }
    /* Exercise public atomic loss requests across the duration of the last
     * accepted frame. Hybrid loss remains 20 ms; SILK loss uses 20/40/60 ms. */
    for(int ch=1;ch<=2;ch++) for(int oc=1;oc<=2;oc++) {
        printf(","); run(0,ch,oc,16000,2,5);
        printf(","); run(0,ch,oc,16000,4,5);
    }
    /* Extend the duration-sensitive pure-mode matrix across every signal,
     * public output rate and channel conversion. The signal-0/16-kHz rows
     * above are retained in place so existing diagnostic case pins stay put. */
    for(int kind=0;kind<6;kind++) for(int ch=1;ch<=2;ch++) for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) {
        if(kind==0&&r==2) continue;
        printf(","); run(kind,ch,oc,rates[r],2,0);
        printf(","); run(kind,ch,oc,rates[r],2,5);
        printf(","); run(kind,ch,oc,rates[r],4,5);
        printf(","); run(kind,ch,oc,rates[r],1,0);
        printf(","); run(kind,ch,oc,rates[r],3,0);
    }
    /* Channel-count changes have their own state boundary; cover them in
     * Hybrid, SILK and both forced mode-transition directions. */
    for(int mode=1;mode<=4;mode++) for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) {
        printf(","); run(0,2,oc,rates[r],mode,4);
    }
    /* Re-encode PCM from RFC 8251 testvector02 through every decoder mode.
     * The broad synthetic matrix above remains the primary boundary coverage;
     * these rows ensure the same loss/recovery path also sees a published,
     * non-synthetic conformance signal. */
    for(int mode=0;mode<=4;mode++) for(int oc=1;oc<=2;oc++) for(int r=0;r<5;r++) {
        printf(","); run(6,2,oc,rates[r],mode,0);
    }
    printf("]}\n");
#ifdef PLC_NO_LOSS_FLOAT
    fprintf(stderr,"]}\n");
#endif
    free(rfc_pcm);
    return 0;
}
