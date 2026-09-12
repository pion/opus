/* SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT */
/* Observation-only linker wrapper around the pinned SILK resampler. */
#include "silk/define.h"
#include "silk/main.h"
#include "silk/SigProc_FIX.h"
static int silk_stage_capture;
static int silk_stage_count;
static opus_int16 silk_stage_input[MAX_FRAME_LENGTH];
static opus_int16 silk_stage_output[MAX_FRAME_LENGTH*6];
static int silk_stage_output_count;
static int silk_core_count;
static opus_int16 silk_core_pulses[MAX_FRAME_LENGTH];
static opus_int32 silk_core_excitation[MAX_FRAME_LENGTH];
static opus_int16 silk_core_pcm[MAX_FRAME_LENGTH];
static opus_int32 silk_core_gains[MAX_NB_SUBFR];
static opus_int16 silk_core_lpc[2*MAX_LPC_ORDER];
static opus_int16 silk_core_ltp[LTP_ORDER*MAX_NB_SUBFR];
static opus_int silk_core_pitch[MAX_NB_SUBFR];
void __real_silk_decode_core(silk_decoder_state *,silk_decoder_control *,opus_int16 *,const opus_int16 *,int);
void __wrap_silk_decode_core(silk_decoder_state *state,silk_decoder_control *control,opus_int16 *out,
    const opus_int16 *pulses,int arch) {
    /* A stereo decode calls the core once per channel before the first
     * resampler call. Keep the first (mid) observation for API-mono probes. */
    int capture=silk_stage_capture && silk_core_count==0;
    if(capture) {
        silk_core_count=state->frame_length;
        memcpy(silk_core_pulses,pulses,silk_core_count*sizeof(*pulses));
        memcpy(silk_core_gains,control->Gains_Q16,state->nb_subfr*sizeof(*control->Gains_Q16));
        memcpy(silk_core_lpc,control->PredCoef_Q12,sizeof(silk_core_lpc));
        memcpy(silk_core_ltp,control->LTPCoef_Q14,state->nb_subfr*LTP_ORDER*sizeof(*control->LTPCoef_Q14));
        memcpy(silk_core_pitch,control->pitchL,state->nb_subfr*sizeof(*control->pitchL));
    }
    __real_silk_decode_core(state,control,out,pulses,arch);
    if(capture) {
        memcpy(silk_core_excitation,state->exc_Q14,silk_core_count*sizeof(*state->exc_Q14));
        memcpy(silk_core_pcm,out,silk_core_count*sizeof(*out));
    }
}
opus_int __real_silk_resampler(silk_resampler_state_struct *,opus_int16 *,const opus_int16 *,opus_int32);
opus_int __wrap_silk_resampler(silk_resampler_state_struct *state,opus_int16 *out,
    const opus_int16 *in,opus_int32 in_len) {
    if(silk_stage_capture) {
        silk_stage_count=in_len;
        memcpy(silk_stage_input,in,in_len*sizeof(*in));
    }
    opus_int result=__real_silk_resampler(state,out,in,in_len);
    if(silk_stage_capture) {
        silk_stage_output_count=in_len*state->Fs_out_kHz/state->Fs_in_kHz;
        memcpy(silk_stage_output,out,silk_stage_output_count*sizeof(*out));
        silk_stage_capture=0;
    }
    return result;
}
static void emit_silk_stage(void) {
    fprintf(stderr,"{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"pre_resample\":[");
    for(int i=0;i<silk_stage_count;i++) fprintf(stderr,"%s%d",i?",":"",silk_stage_input[i]);
    fprintf(stderr,"],\"post_resample\":[");
    for(int i=0;i<silk_stage_output_count;i++) fprintf(stderr,"%s%d",i?",":"",silk_stage_output[i]);
    fprintf(stderr,"],\"pulses\":[");
    for(int i=0;i<silk_core_count;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_pulses[i]);
    fprintf(stderr,"],\"excitation_q14\":[");
    for(int i=0;i<silk_core_count;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_excitation[i]);
    fprintf(stderr,"],\"core_pcm\":[");
    for(int i=0;i<silk_core_count;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_pcm[i]);
    fprintf(stderr,"],\"gains_q16\":[");
    for(int i=0;i<MAX_NB_SUBFR;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_gains[i]);
    fprintf(stderr,"],\"lpc_q12\":[");
    for(int i=0;i<2*MAX_LPC_ORDER;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_lpc[i]);
    fprintf(stderr,"],\"ltp_q14\":[");
    for(int i=0;i<LTP_ORDER*MAX_NB_SUBFR;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_ltp[i]);
    fprintf(stderr,"],\"pitch\":[");
    for(int i=0;i<MAX_NB_SUBFR;i++) fprintf(stderr,"%s%d",i?",":"",silk_core_pitch[i]);
    fprintf(stderr,"]}\n");
}
