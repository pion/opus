/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly> */
/* SPDX-License-Identifier: MIT */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

#include "silk/API.h"

#ifdef SILK_STAGE
#include "silk-stage.h"
#endif

#define PIN "22244de5a79bd1d6d623c32e72bf1954b56235be"

static void check(int value) {
    if(value < 0) {
        fprintf(stderr, "opus error %d\n", value);
        exit(1);
    }
}

int main(void) {
    static const unsigned char frames[][28] = {
        {0x0b,0xe4,0xc1,0x36,0xec,0xc5,0x80},
        {0x07,0xc9,0x72,0x27,0xe1,0x44,0xea,0x50},
    };
    static const int lengths[] = {7, 8};
    int decoder_size;
    check(silk_Get_Decoder_Size(&decoder_size));
    void *decoder = calloc(1, decoder_size);
    if(decoder == NULL) return 3;
    check(silk_InitDecoder(decoder));

    silk_DecControlStruct control;
    memset(&control, 0, sizeof(control));
    control.nChannelsAPI = 1;
    control.nChannelsInternal = 1;
    control.API_sampleRate = 16000;
    control.internalSampleRate = 16000;
    control.payloadSize_ms = 20;

#ifdef SILK_PLC
    static const int lost_flags[] = {0, 1, 1, 0};
    printf("{\"pin\":\"%s\",\"frames\":[", PIN);
    for(int step = 0; step < 4; step++) {
        int frame = step == 3 ? 1 : 0;
        ec_dec range_decoder;
        opus_res pcm[320];
        opus_int32 samples = 0;
        ec_dec_init(&range_decoder, (unsigned char *)frames[frame], lengths[frame]);
        silk_stage_capture = 1;
        check(silk_Decode(decoder, &control, lost_flags[step], 1, &range_decoder, pcm, &samples, 0));
        if(samples != 320) return 2;
        printf("%s[", step ? "," : "");
        for(int i = 0; i < samples; i++)
            printf("%s%d", i ? "," : "", silk_stage_input[i]);
        printf("]");
    }
    printf("]}\n");
#else
    printf("{\"pin\":\"%s\",\"frames\":[", PIN);
    for(int frame = 0; frame < 2; frame++) {
        ec_dec range_decoder;
        opus_res pcm[320];
        opus_int32 samples = 0;
        ec_dec_init(&range_decoder, (unsigned char *)frames[frame], lengths[frame]);
#ifdef SILK_STAGE
        silk_stage_capture = 1;
#endif
        check(silk_Decode(decoder, &control, 0, 1, &range_decoder, pcm, &samples, 0));
#ifdef SILK_STAGE
        emit_silk_stage();
#endif
        if(samples != 320) return 2;
        printf("%s[", frame ? "," : "");
        for(int i = 0; i < samples; i++) {
#ifdef SILK_STAGE
            printf("%s%d", i ? "," : "", silk_stage_input[i]);
#else
            printf("%s%d", i ? "," : "", (int)lrintf(pcm[i] * 32768.0f));
#endif
        }
        printf("]");
    }
    printf("]}\n");
#endif
    free(decoder);
    return 0;
}
