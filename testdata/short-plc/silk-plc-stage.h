/* SPDX-FileCopyrightText: 2006-2011 Skype Limited
 * SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
 * SPDX-License-Identifier: MIT AND BSD-3-Clause */
/* Observation-only wrapper around the pinned non-neural SILK frame decoder. */
#include "silk/main.h"

static int silk_plc_state_capture;
static silk_decoder_state silk_plc_decoder;
static opus_int16 silk_plc_frame[MAX_FRAME_LENGTH];
static int silk_plc_frame_length;

opus_int __real_silk_decode_frame(silk_decoder_state *, ec_dec *, opus_int16 *, opus_int32 *, opus_int, opus_int, int);
opus_int __wrap_silk_decode_frame(silk_decoder_state *state, ec_dec *range, opus_int16 *out,
    opus_int32 *count, opus_int lost, opus_int coding, int arch) {
    opus_int result=__real_silk_decode_frame(state,range,out,count,lost,coding,arch);
    if(silk_plc_state_capture) {
        silk_plc_decoder=*state;
        silk_plc_frame_length=*count;
        memcpy(silk_plc_frame,out,*count*sizeof(*out));
        silk_plc_state_capture=0;
    }
    return result;
}

static void silk_plc_i16(const opus_int16 *values,int count) {
    fprintf(stderr,"[");
    for(int i=0;i<count;i++) fprintf(stderr,"%s%d",i?",":"",values[i]);
    fprintf(stderr,"]");
}
static void silk_plc_i32(const opus_int32 *values,int count) {
    fprintf(stderr,"[");
    for(int i=0;i<count;i++) fprintf(stderr,"%s%d",i?",":"",values[i]);
    fprintf(stderr,"]");
}
static void emit_silk_plc_state(void) {
    silk_decoder_state *state=&silk_plc_decoder;
    fprintf(stderr,"{\"pin\":\"22244de5a79bd1d6d623c32e72bf1954b56235be\",\"frame\":");
    silk_plc_i16(silk_plc_frame,silk_plc_frame_length);
    fprintf(stderr,",\"slpc_q14\":"); silk_plc_i32(state->sLPC_Q14_buf,MAX_LPC_ORDER);
    fprintf(stderr,",\"out_buf\":"); silk_plc_i16(state->outBuf,state->ltp_mem_length);
    fprintf(stderr,",\"exc_q14\":"); silk_plc_i32(state->exc_Q14,state->frame_length);
    fprintf(stderr,",\"plc_lpc_q12\":"); silk_plc_i16(state->sPLC.prevLPC_Q12,state->LPC_order);
    fprintf(stderr,",\"plc_ltp_q14\":"); silk_plc_i16(state->sPLC.LTPCoef_Q14,LTP_ORDER);
    fprintf(stderr,",\"plc_pitch_q8\":%d,\"plc_gain_q16\":[%d,%d],\"plc_ltp_scale_q14\":%d,"
        "\"plc_seed\":%d,\"plc_random_scale_q14\":%d,\"previous_gain_q16\":%d,"
        "\"lag_previous\":%d,\"loss_count\":%d}\n",
        state->sPLC.pitchL_Q8,state->sPLC.prevGain_Q16[0],state->sPLC.prevGain_Q16[1],
        state->sPLC.prevLTP_scale_Q14,state->sPLC.rand_seed,state->sPLC.randScale_Q14,
        state->prev_gain_Q16,state->lagPrev,state->lossCnt);
}
