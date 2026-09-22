/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Generic pinned libopus, -O2 -ffp-contract=off; see EXACTNESS.md. */
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include "opus.h"

static void check(int result) {
    if (result<0) { fprintf(stderr,"%s\n",opus_strerror(result)); exit(1); }
}

static OpusEncoder *encoder(int channels, int mode, int bandwidth) {
    int error;
    OpusEncoder *enc=opus_encoder_create(48000,channels,OPUS_APPLICATION_AUDIO,&error);
    check(error);
    check(opus_encoder_ctl(enc,11002,mode));
    check(opus_encoder_ctl(enc,OPUS_SET_BANDWIDTH(bandwidth)));
    check(opus_encoder_ctl(enc,OPUS_SET_BITRATE(32000*channels)));
    check(opus_encoder_ctl(enc,OPUS_SET_VBR(0)));
    check(opus_encoder_ctl(enc,OPUS_SET_DTX(0)));
    return enc;
}

static void run(int profile, int channels, int rate) {
    const int modes[]={1000,1000,1000,1001,1001,1002,1002,1002,1002,1000,1000,1002};
    const int bands[]={1101,1102,1103,1104,1105,1101,1103,1104,1105,1101,1103,1105};
    int error, band=bands[profile], mode=modes[profile];
    OpusEncoder *enc=encoder(channels,mode,band);
    OpusDecoder *dec=opus_decoder_create(rate,channels,&error); check(error);
    OpusDecoder *probe=opus_decoder_create(rate,channels,&error); check(error);
    check(opus_decoder_ctl(dec,OPUS_SET_PHASE_INVERSION_DISABLED(0)));
    check(opus_decoder_ctl(probe,OPUS_SET_PHASE_INVERSION_DISABLED(0)));
    printf("{\"profile\":%d,\"channels\":%d,\"rate\":%d,\"steps\":[",profile,channels,rate);
    for(int step=0;step<12;step++) {
        /* Fresh independent packets make internal-rate changes deterministic,
         * without involving the separately tracked mode/redundancy transitions. */
        if(profile==9||profile==10) {
            int next=profile==9 ? 1101+step/4 : 1103-step/4;
            if(next!=band) {
                band=next;
                opus_encoder_destroy(enc);
                enc=encoder(channels,mode,band);
            }
        }
        float input[1920], float_out[1920];
        opus_int16 out[1920]; unsigned char packet[1275];
        for(int i=0;i<960;i++) for(int ch=0;ch<channels;ch++) {
            int position=step*960+i;
            float sample=(float)((position%(120+ch*31))-60)/600.f;
            if(profile==11) sample=(float)(2.7*sin(position*(0.035+ch*0.004)));
            input[i*channels+ch]=sample;
        }
        int bytes=opus_encode_float(enc,input,960,packet,sizeof(packet)); check(bytes);
        if(opus_packet_get_bandwidth(packet)!=band) {
            fprintf(stderr,"profile %d step %d: requested bandwidth %d got %d\n",profile,step,band,opus_packet_get_bandwidth(packet));
            exit(2);
        }
        int lost=step==3||step==5||step==9||step==10;
        int n=opus_decode(dec,lost?NULL:packet,lost?0:bytes,out,rate/50,0); check(n);
        int fn=opus_decode_float(probe,lost?NULL:packet,lost?0:bytes,float_out,rate/50,0); check(fn);
        if(n!=rate/50||fn!=n) exit(3);
        opus_uint32 range; check(opus_decoder_ctl(dec,OPUS_GET_FINAL_RANGE(&range)));
        int clipped=0;
        for(int i=0;i<n*channels;i++) if(fabsf(float_out[i])>1.f) clipped=1;
        printf("%s{\"bandwidth\":%d,\"clipped\":%s,\"packet\":\"",step?",":"",band,clipped?"true":"false");
        if(!lost) for(int i=0;i<bytes;i++) printf("%02x",packet[i]);
        printf("\",\"samples\":%d,\"range\":%u,\"pcm\":[",n,range);
        for(int i=0;i<n*channels;i++) printf("%s%d",i?",":"",out[i]);
        printf("]}");
    }
    printf("]}");
    opus_encoder_destroy(enc); opus_decoder_destroy(dec); opus_decoder_destroy(probe);
}

int main(void) {
    int rates[]={8000,12000,16000,24000,48000}, first=1;
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
    for(int profile=0;profile<12;profile++) for(int channels=1;channels<=2;channels++) for(int r=0;r<5;r++) {
        if(!first) putchar(','); first=0;
        run(profile,channels,rates[r]);
    }
    puts("]}");
    return 0;
}
