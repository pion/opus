/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Observation-only wrappers around the pinned generic CELT PLC filters. */
#include <stdint.h>
#include <string.h>
#include "celt/celt_decoder.c"

static int celt_plc_math_capture;

static void celt_plc_math_bits(const float *values, int count) {
    fprintf(stderr, "[");
    for (int i=0; i<count; i++) {
        uint32_t bits;
        memcpy(&bits, &values[i], sizeof(bits));
        fprintf(stderr, "%s%u", i ? "," : "", bits);
    }
    fprintf(stderr, "]");
}

void __real_celt_fir_c(const float *, const float *, float *, int, int, int);
void __wrap_celt_fir_c(const float *input, const float *coefficients, float *output,
    int count, int order, int arch) {
    __real_celt_fir_c(input, coefficients, output, count, order, arch);
    if (!celt_plc_math_capture) return;
    fprintf(stderr, "{\"fir_input\":");
    celt_plc_math_bits(input-order, count+order);
    fprintf(stderr, ",\"coefficients\":");
    celt_plc_math_bits(coefficients, order);
    fprintf(stderr, ",\"fir_output\":");
    celt_plc_math_bits(output, count);
}

void __real_celt_iir(const float *, const float *, float *, int, int, float *, int);
void __wrap_celt_iir(const float *input, const float *coefficients, float *output,
    int count, int order, float *memory, int arch) {
    if (celt_plc_math_capture) {
        fprintf(stderr, ",\"iir_input\":");
        celt_plc_math_bits(input, count);
        fprintf(stderr, ",\"iir_memory\":");
        celt_plc_math_bits(memory, order);
    }
    __real_celt_iir(input, coefficients, output, count, order, memory, arch);
    if (!celt_plc_math_capture) return;
    fprintf(stderr, ",\"iir_output\":");
    celt_plc_math_bits(output, count);
    fprintf(stderr, "}\n");
    celt_plc_math_capture = 0;
}
