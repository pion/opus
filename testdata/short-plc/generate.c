/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
#include <stdio.h>
#include <stdlib.h>
#include "opus.h"

static void check(int result) {
    if (result < 0) {
        fprintf(stderr, "%s\n", opus_strerror(result));
        exit(1);
    }
}

static void run_case(int channels, int divisor, int transition) {
    int error, offset = 0;
    OpusEncoder *encoder = opus_encoder_create(48000, channels,
        transition ? OPUS_APPLICATION_AUDIO : OPUS_APPLICATION_RESTRICTED_LOWDELAY, &error);
    check(error);
    OpusDecoder *decoder = opus_decoder_create(16000, channels, &error);
    check(error);
    check(opus_encoder_ctl(encoder, OPUS_SET_BITRATE(64000 * channels)));
    check(opus_encoder_ctl(encoder, OPUS_SET_VBR(0)));
    check(opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(10)));
    check(opus_encoder_ctl(encoder, OPUS_SET_DTX(0)));
    /* Test-only control from the pinned src/opus_private.h. */
    if (transition) {
        check(opus_encoder_ctl(encoder, 11002, 1000));
        check(opus_encoder_ctl(encoder, OPUS_SET_BANDWIDTH(OPUS_BANDWIDTH_WIDEBAND)));
    }
    printf("{\"channels\":%d,\"rate\":16000,\"divisor\":%d,\"transition\":%s,\"steps\":[",
        channels, divisor, transition ? "true" : "false");
    for (int step = 0; step < 8; step++) {
        int lost = step >= 3 && step < 6;
        int samples = lost ? 48000 / divisor : 960;
        opus_int16 input[1920], output[640];
        unsigned char packet[1275];
        if (transition && step == 2) check(opus_encoder_ctl(encoder, 11002, 1002));
        for (int i = 0; i < samples; i++) {
            for (int channel = 0; channel < channels; channel++) {
                input[i * channels + channel] = (opus_int16)(((i + offset) % (80 + channel * 31) - 40) * 80);
            }
        }
        offset += samples;
        int bytes = opus_encode(encoder, input, samples, packet, sizeof(packet));
        check(bytes);
        if (!transition && (packet[0] >> 3) < 16) exit(2);
        int decoded = opus_decode(decoder, lost ? NULL : packet, lost ? 0 : bytes, output, samples / 3, 0);
        check(decoded);
        if (decoded != samples / 3) exit(3);
        opus_uint32 final_range;
        check(opus_decoder_ctl(decoder, OPUS_GET_FINAL_RANGE(&final_range)));
        printf("%s{\"packet\":\"", step ? "," : "");
        if (!lost) for (int i = 0; i < bytes; i++) printf("%02x", packet[i]);
        printf("\",\"samples\":%d,\"range\":%u,\"pcm\":[", decoded, final_range);
        for (int i = 0; i < decoded * channels; i++) printf("%s%d", i ? "," : "", output[i]);
        printf("]}");
    }
    printf("]}");
    opus_encoder_destroy(encoder);
    opus_decoder_destroy(decoder);
}

int main(void) {
    const int divisors[] = {400, 200, 100, 50};
    printf("{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"cases\":[");
    for (int channels = 1; channels <= 2; channels++) {
        for (int i = 0; i < 4; i++) {
            if (channels != 1 || i != 0) printf(",");
            run_case(channels, divisors[i], 0);
        }
    }
    for (int i = 0; i < 3; i++) {
        printf(",");
        run_case(1, divisors[i], 1);
    }
    puts("]}");
    return 0;
}
