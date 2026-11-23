#!/usr/bin/env python3
"""
Real-Time Streaming Offline Japanese Speech-to-Text Extractor (SSE Ready)
Targets ONLY Japanese speech (language="ja") from video/audio using local Faster-Whisper
with Japanese Kanji/Contextual Prompting, Per-Sentence Timestamp Splitting,
and Homophone/Katakana Normalization, flushing each sentence segment immediately to stdout as NDJSON.
"""

import argparse
import json
import os
import re
import sys
from faster_whisper import WhisperModel

DEFAULT_MODEL_PATH = "/home/hideri/coding/Open-LLM-VTuber/models/whisper_base"
JP_CHAR_REGEX = re.compile(r"[\u3040-\u309F\u30A0-\u30FF\u4E00-\u9FFF]")

# Contextual prompt guides Whisper to prefer standard Japanese Kanji (広報, 生徒会, 了解, 先輩, etc.)
ANIME_INITIAL_PROMPT = (
    "日本のアニメの日常会話字幕。生徒会、広報活動、広報、ポスター、張り替える、肝心の、先輩、後輩、"
    "了解です、さっき、今起きたって連絡が、やっぱスマホ忘れたかも、スマホ濡れたかも、今日のお詫びも兼ねて、"
    "本当に、大丈夫、ありがとう。"
)

HOMOPHONE_CORRECTIONS = [
    ("コウホー活動", "広報活動"),
    ("コウホー", "広報"),
    ("こうほう活動", "広報活動"),
    ("張り返る", "張り替える"),
    ("漢人の", "肝心の"),
    ("そっき", "さっき"),
    ("先今起きた", "さっき今起きた"),
    ("スマホを塗れた", "スマホ濡れた"),
    ("スマホ塗れた", "スマホ濡れた"),
    ("スマホ乗れた", "スマホ濡れた"),
    ("今日のお湧きも金て", "今日のお詫びも兼ねて"),
    ("お湧きも金て", "お詫びも兼ねて"),
    ("矢解です", "了解です"),
    ("妖怪です", "了解です"),
    ("りょうかいです", "了解です"),
    ("決会して", "結果して"),
]


def normalize_japanese_transcript(text: str) -> str:
    cleaned = text.strip()
    for wrong, right in HOMOPHONE_CORRECTIONS:
        cleaned = cleaned.replace(wrong, right)
    return cleaned


def split_into_sentences_with_timestamps(text: str, start_sec: float, end_sec: float):
    """
    If a Whisper segment spans multiple sentences separated by 。 / ？ / ！ / ? / !,
    splits them into individual sentence cues with proportional timestamps.
    """
    parts = re.split(r"(?<=[。？！?!])\s*", text.strip())
    sentences = [p.strip() for p in parts if p and p.strip() and JP_CHAR_REGEX.search(p)]
    if len(sentences) <= 1:
        return [(text.strip(), start_sec, end_sec)]

    total_chars = sum(len(s) for s in sentences)
    if total_chars <= 0:
        return [(text.strip(), start_sec, end_sec)]

    dur = max(0.8, end_sec - start_sec)
    result = []
    cur_start = start_sec
    for i, s in enumerate(sentences):
        if i == len(sentences) - 1:
            s_end = end_sec
        else:
            s_end = cur_start + dur * (len(s) / total_chars)
        result.append((s, cur_start, s_end))
        cur_start = s_end
    return result


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
        "progress": 12,
        "message": "Memuat model AI Faster-Whisper Offline (Target: Bahasa Jepang / Kanji Kontekstual)...",
    })

    model_dir = args.model if os.path.exists(args.model) else "base"
    model = WhisperModel(model_dir, device="cpu", compute_type="int8", cpu_threads=4)

    emit({
        "type": "status",
        "stage": "transcribing",
        "progress": 16,
        "message": "Memindai seluruh percakapan Bahasa Jepang dari video (VAD + Kanji Context aktif)...",
    })

    segments, info = model.transcribe(
        args.input,
        language="ja",
        task="transcribe",
        initial_prompt=ANIME_INITIAL_PROMPT,
        vad_filter=True,
        vad_parameters=dict(
            min_silence_duration_ms=250,
            speech_pad_ms=180,
            max_speech_duration_s=5.5,
        ),
        beam_size=2,
        best_of=1,
        condition_on_previous_text=False,
        temperature=0.0,
    )

    total_dur = max(1.0, float(info.duration or 1.0))
    emit({
        "type": "info",
        "language": "ja",
        "duration_sec": round(total_dur, 2),
        "progress": 18,
        "message": f"Durasi video terdeteksi: {int(total_dur // 60):02d}:{int(total_dur % 60):02d}. Mengekstrak kalimat Jepang secara real-time...",
    })

    idx = 1
    srt_blocks = []

    for seg in segments:
        norm_text = normalize_japanese_transcript(seg.text)
        if not norm_text or not JP_CHAR_REGEX.search(norm_text):
            continue

        sub_sentences = split_into_sentences_with_timestamps(norm_text, seg.start, seg.end)
        for sent_text, s_start, s_end in sub_sentences:
            start_ms = args.offset_ms + int(round(s_start * 1000))
            end_ms = args.offset_ms + int(round(s_end * 1000))
            if end_ms <= start_ms:
                end_ms = start_ms + 1500

            start_srt = format_srt_time(start_ms / 1000.0)
            end_srt = format_srt_time(end_ms / 1000.0)
            srt_block = f"{idx}\n{start_srt} --> {end_srt}\n{sent_text}"
            srt_blocks.append(srt_block)

            pct = min(98, 18 + int(round((s_end / total_dur) * 80)))
            cur_min = int((start_ms // 1000) // 60)
            cur_sec = int((start_ms // 1000) % 60)
            tot_min = int(((args.offset_ms // 1000) + total_dur) // 60)
            tot_sec = int(((args.offset_ms // 1000) + total_dur) % 60)

            emit({
                "type": "segment",
                "index": idx,
                "start_time_ms": start_ms,
                "end_time_ms": end_ms,
                "sentence": sent_text,
                "srt_block": srt_block,
                "progress": pct,
                "current_sec": round(s_end, 2),
                "total_sec": round(total_dur, 2),
                "message": f"Menyalin kalimat Jepang #{idx} (Posisi {cur_min:02d}:{cur_sec:02d} / {tot_min:02d}:{tot_sec:02d})...",
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
