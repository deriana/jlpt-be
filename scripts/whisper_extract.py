#!/usr/bin/env python3
"""
Real-Time Streaming Offline Japanese Speech-to-Text Extractor (SSE Ready)
Targets ONLY Japanese speech (language="ja") from video/audio using local Faster-Whisper
and flushes each sentence segment immediately to stdout as NDJSON (Newline-Delimited JSON).
"""

import argparse
import json
import os
import re
import sys
from faster_whisper import WhisperModel

DEFAULT_MODEL_PATH = "/home/hideri/coding/Open-LLM-VTuber/models/whisper_base"
JP_CHAR_REGEX = re.compile(r"[\u3040-\u309F\u30A0-\u30FF\u4E00-\u9FFF]")


def emit(payload: dict):
    print(json.dumps(payload, ensure_ascii=False), flush=True)


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
    parser = argparse.ArgumentParser(description="Stream Japanese sentence timestamps from audio")
    parser.add_argument("--input", required=True, help="Path to input 16kHz WAV file")
    parser.add_argument("--model", default=DEFAULT_MODEL_PATH, help="Path to local faster-whisper model")
    parser.add_argument("--offset-ms", type=int, default=0, help="Start offset in milliseconds")
    args = parser.parse_args()

    if not os.path.exists(args.input):
        emit({"type": "error", "message": f"File audio tidak ditemukan: {args.input}"})
        sys.exit(1)

    emit({
        "type": "status",
        "stage": "loading_model",
        "progress": 15,
        "message": "Memuat model AI Faster-Whisper Offline (Target: Bahasa Jepang / ja)...",
    })

    model_dir = args.model if os.path.exists(args.model) else "base"
    model = WhisperModel(model_dir, device="cpu", compute_type="int8", cpu_threads=4)

    emit({
        "type": "status",
        "stage": "transcribing",
        "progress": 20,
        "message": "Memindai percakapan Bahasa Jepang dari audio video (VAD aktif)...",
    })

    segments, info = model.transcribe(
        args.input,
        language="ja",
        task="transcribe",
        vad_filter=True,
        vad_parameters=dict(min_silence_duration_ms=300, speech_pad_ms=200),
        beam_size=1,
        best_of=1,
        condition_on_previous_text=False,
    )

    total_dur = max(1.0, float(info.duration or 1.0))
    emit({
        "type": "info",
        "language": "ja",
        "duration_sec": round(total_dur, 2),
        "progress": 22,
        "message": f"Durasi audio terdeteksi: {int(total_dur // 60):02d}:{int(total_dur % 60):02d}. Mengekstrak kalimat Jepang...",
    })

    idx = 1
    srt_blocks = []

    for seg in segments:
        text = seg.text.strip()
        # Only keep segments that actually contain Japanese characters (Hiragana/Katakana/Kanji)
        if not text or not JP_CHAR_REGEX.search(text):
            continue

        start_ms = args.offset_ms + int(round(seg.start * 1000))
        end_ms = args.offset_ms + int(round(seg.end * 1000))
        if end_ms <= start_ms:
            end_ms = start_ms + 1500

        start_srt = format_srt_time(start_ms / 1000.0)
        end_srt = format_srt_time(end_ms / 1000.0)
        srt_block = f"{idx}\n{start_srt} --> {end_srt}\n{text}"
        srt_blocks.append(srt_block)

        pct = min(98, 22 + int(round((seg.end / total_dur) * 76)))
        cur_min = int(seg.end // 60)
        cur_sec = int(seg.end % 60)
        tot_min = int(total_dur // 60)
        tot_sec = int(total_dur % 60)

        emit({
            "type": "segment",
            "index": idx,
            "start_time_ms": start_ms,
            "end_time_ms": end_ms,
            "sentence": text,
            "srt_block": srt_block,
            "progress": pct,
            "current_sec": round(seg.end, 2),
            "total_sec": round(total_dur, 2),
            "message": f"Menyalin kalimat Jepang #{idx} ({cur_min:02d}:{cur_sec:02d} / {tot_min:02d}:{tot_sec:02d})...",
        })
        idx += 1

    emit({
        "type": "done",
        "progress": 100,
        "total_segments": idx - 1,
        "srt": "\n\n".join(srt_blocks),
        "message": f"Selesai! Berhasil mengekstrak {idx - 1} kalimat Bahasa Jepang dari video.",
    })


if __name__ == "__main__":
    main()
