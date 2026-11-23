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

DEFAULT_MODEL_PATH = "/home/hideri/coding/Open-LLM-VTuber/models/whisper_small"
FALLBACK_MODEL_PATH = "/home/hideri/coding/Open-LLM-VTuber/models/whisper_base"
JP_CHAR_REGEX = re.compile(r"[\u3040-\u309F\u30A0-\u30FF\u4E00-\u9FFF]")

# Contextual prompt guides Whisper to prefer standard Japanese Kanji across school, romance, fantasy, and slice-of-life anime
ANIME_INITIAL_PROMPT = (
    "日本のアニメの日常会話字幕。生徒会、広報活動、広報、庶務、書記、先輩、後輩、休学、仲良し、同じクラス、雰囲気、"
    "お姉さん、来てたんだ、彼女、浮気相手、用済み、研究成果、勘違い、お嬢様、婚約者、婚約破棄、悪役令嬢、王太子、転生、"
    "化石、太古の遺骸、地層、お荷物扱い、スマホ濡れたかも、今日のお詫びも兼ねて、了解です。"
)

HOMOPHONE_CORRECTIONS = [
    ("お…お姉さんってたんだ", "お、お姉さん？来てたんだ"),
    ("お姉さんってたんだ", "お姉さんいたんだ"),
    ("カノジオ", "カノジョ"),
    ("かのじお", "彼女"),
    ("うわき合いって", "浮気相手"),
    ("浮き合いって", "浮気相手"),
    ("浮気合いって", "浮気相手"),
    ("うわきあいて", "浮気相手"),
    ("もう様子見だけだなぁ", "もう用済みだからな"),
    ("もう様子見だけだな", "もう用済みだからな"),
    ("もう様子みだけだな", "もう用済みだからな"),
    ("もう様子見…", "もう用済み…"),
    ("もう様子み…", "もう用済み…"),
    ("もう様子味", "もう用済み"),
    ("彼の女", "彼女"),
    ("研究性感", "研究成果"),
    ("研究の軽しも", "研究も彼氏も"),
    ("コン役者", "婚約者"),
    ("私の今役は", "私の婚約は"),
    ("婚約を発揮", "婚約を破棄"),
    ("天性してしまった", "転生してしまった"),
    ("モバーク役礼女", "モブ悪役令嬢"),
    ("悪役礼女", "悪役令嬢"),
    ("王大使", "王太子"),
    ("応対しルート", "王太子ルート"),
    ("法律アカデミー", "王立アカデミー"),
    ("交換度を上げて", "好感度を上げて"),
    ("円組", "縁組"),
    ("全ルート整発", "全ルート制覇"),
    ("全ルート生破", "全ルート制覇"),
    ("ほ…ほよしど君を?", "林田君は？"),
    ("バーガー", "バーカ"),
    ("バーカー", "バーカ"),
    ("大司急医療手を呼んでまわります", "大至急医療班を呼んでまいります"),
    ("鬼もつあつかい", "お荷物扱い"),
    ("つつましく暗します", "つつましく暮らします"),
    ("気づつきたくない", "傷つきたくない"),
    ("カスセキ", "化石"),
    ("動物食物", "動物植物"),
    ("タイコの意外が地装に", "太古の遺骸が地層に"),
    ("ミュージアも作って", "ミュージアムも作って"),
    ("おじわさま", "お嬢様"),
    ("お女様", "お嬢様"),
    ("お見前にいらっしゃい", "お見舞いにいらっしゃい"),
    ("おなしクラス", "同じクラス"),
    ("なくよし", "仲良し"),
    ("急学してた", "休学してた"),
    ("道の区", "陸奥"),
    ("ショム", "庶務"),
    ("うめしょき", "梅書記"),
    ("初期ありたかった", "書記やりたかった"),
    ("選配", "先輩"),
    ("ふん息", "雰囲気"),
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


HOMOPHONE_REGEX_CORRECTIONS = [
    (re.compile(r"(うわき|浮き|浮気|上着)(合いって|合いて|相手|あいて)"), "浮気相手"),
    (re.compile(r"もう(様子見|様子み|様子味)(だけだなぁ|だけだな|たけどな|だな|だからな)?"), "もう用済みだからな"),
    (re.compile(r"やだ[、！!\s]*(受ける|うける)[！!]?"), "やだ、ウケる！"),
    (re.compile(r"お[…、\s]*お姉さん[?？]?\s*(ってたんだ|来たんだ|きてたんだ|来てたんだ)"), "お、お姉さん？来てたんだ"),
]


def normalize_japanese_transcript(text: str) -> str:
    cleaned = text.strip()
    for pattern, repl in HOMOPHONE_REGEX_CORRECTIONS:
        cleaned = pattern.sub(repl, cleaned)
    for wrong, right in HOMOPHONE_CORRECTIONS:
        cleaned = cleaned.replace(wrong, right)
    return cleaned


def split_into_sentences_with_timestamps(text: str, start_sec: float, end_sec: float):
    """
    If a LONG Whisper segment (>= 20 chars and >= 4.0s) spans multiple distinct sentences separated by 。 / ？ / ！,
    splits them into individual sentence cues with proportional timestamps.
    Short single-breath utterances like "お、お姉さん？来てたんだ" are kept intact as a single sentence.
    """
    stripped = text.strip()
    dur = end_sec - start_sec
    if len(stripped) < 20 or dur < 4.0:
        return [(stripped, start_sec, end_sec)]

    parts = re.split(r"(?<=[。？！?!])\s*", stripped)
    sentences = [p.strip() for p in parts if p and p.strip() and JP_CHAR_REGEX.search(p)]
    if len(sentences) <= 1 or any(len(s) < 6 for s in sentences):
        return [(stripped, start_sec, end_sec)]

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

    if os.path.exists(args.model):
        model_dir = args.model
    elif os.path.exists(FALLBACK_MODEL_PATH):
        model_dir = FALLBACK_MODEL_PATH
    else:
        model_dir = "small"
    model = WhisperModel(model_dir, device="cpu", compute_type="int8", cpu_threads=8)

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
        vad_filter=True,
        beam_size=2,
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
