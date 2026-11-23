#!/usr/bin/env python3
"""
Offline Video/Audio Speech-to-Text Subtitle & Timestamp Extractor
Uses local Faster-Whisper model (/home/hideri/coding/Open-LLM-VTuber/models/whisper_base)
to transcribe Japanese speech from uploaded video files into exact sentence timestamps.
"""

import argparse
import json
import os
import sys
from faster_whisper import WhisperModel

DEFAULT_MODEL_PATH = "/home/hideri/coding/Open-LLM-VTuber/models/whisper_base"


def format_srt_time(seconds: float) -> str:
    if seconds < 0:
        seconds = 0.0
    total_ms = int(round(seconds * 1000))
    ms = total_ms % 1000
    total_sec = total_ms // 1000
    s = total_sec % 60
    total_min = total_sec // 60
    m = total_min % 60
    h = total_min // 60
    return f"{h:02d}:{m:02d}:{s:02d},{ms:03d}"


def main():
    parser = argparse.ArgumentParser(description="Extract timestamped Japanese sentences from audio/video")
    parser.add_argument("--input", required=True, help="Path to input WAV/video file")
    parser.add_argument("--model", default=DEFAULT_MODEL_PATH, help="Path to local faster-whisper model")
    parser.add_argument("--lang", default="ja", help="Language code (default: ja)")
    args = parser.parse_args()

    if not os.path.exists(args.input):
        print(json.dumps({"error": f"Input file not found: {args.input}"}))
        sys.exit(1)

    model_dir = args.model if os.path.exists(args.model) else "base"
    model = WhisperModel(model_dir, device="cpu", compute_type="int8")

    segments, info = model.transcribe(
        args.input,
        language=args.lang if args.lang != "auto" else None,
        vad_filter=True,
        vad_parameters=dict(min_silence_duration_ms=350),
        beam_size=3,
    )

    srt_blocks = []
    cue_items = []
    idx = 1

    for seg in segments:
        text = seg.text.strip()
        if not text:
            continue
        start_ms = int(round(seg.start * 1000))
        end_ms = int(round(seg.end * 1000))
        if end_ms <= start_ms:
            end_ms = start_ms + 1500

        start_srt = format_srt_time(seg.start)
        end_srt = format_srt_time(seg.end)
        srt_blocks.append(f"{idx}\n{start_srt} --> {end_srt}\n{text}")
        cue_items.append({
            "index": idx,
            "start_time_ms": start_ms,
            "end_time_ms": end_ms,
            "sentence": text,
        })
        idx += 1

    output = {
        "language": info.language,
        "duration_sec": round(info.duration, 2),
        "srt": "\n\n".join(srt_blocks),
        "segments": cue_items,
    }
    print(json.dumps(output, ensure_ascii=False))


if __name__ == "__main__":
    main()
