# Demo video capture and GIF conversion

How to capture a gameplay video on macOS, encode it as a small MP4 that
plays inline on GitHub, and produce an animated GIF preview that can be
embedded in the README or linked from communities (e.g. r/zxspectrum)
that don't allow video uploads.

This is the exact pipeline used to produce `media/atic-atac-demo.mp4`
and `media/atic-atac-demo.gif`. Run from the repo root.

## Prerequisites

- macOS 13+ (Sound capture in OBS uses ScreenCaptureKit, available 13+).
- [OBS Studio](https://obsproject.com/) 30.2 or newer (older versions
  don't expose the `Capture Audio` toggle on the macOS Screen Capture
  source).
- `ffmpeg` for encoding/cropping/GIF generation:
  ```sh
  brew install ffmpeg
  ```
- The game's display window. Resize it to an integer multiple of the
  internal 256×192 frame (e.g. 4× = 1024×768, 5× = 1280×960) before
  recording — non-integer scales blur the pixel art.

## 1. OBS one-time setup

System Settings → Privacy & Security → **Screen Recording** → enable
OBS. Quit and relaunch OBS after toggling.

In OBS:

1. **Sources → +** → `macOS Screen Capture (SCK based)`.
2. In Properties:
   - **Method:** ScreenCaptureKit
   - **Type:** Window
   - **Window:** the running game
   - **Capture Audio:** ✅ (this is the inline-audio toggle that only
     exists on OBS 30.2+; if missing, install BlackHole and add an
     `Audio Input Capture` source pointing at it instead)
3. **Settings → Output**
   - Recording Format: `mp4`
   - Encoder: `Apple VT H.264 Hardware`
4. **Settings → Video**
   - Base + Output Resolution: your display resolution
   - FPS: `50` (matches the game's PAL tick rate; 60 also fine but
     resamples)
5. **Settings → Audio**
   - Sample rate: `48 kHz`

## 2. Record

1. In the OBS preview, drag/scale the game window source so the
   playable area fills the canvas — no large empty margins (they'll
   need to be cropped out later).
2. **Start Recording**, play, **Stop Recording**.
3. Recording lands in `Settings → Output → Recording Path` (default
   `~/Movies`).
4. Quick sanity check: open the file in QuickTime, confirm the audio
   meter moves during playback.

## 3. Re-encode the MP4 (size + GitHub inline player)

GitHub's blob viewer renders an inline HTML5 video player only when the
file is under ~5 MB. The raw OBS capture is usually much larger; even
with sensible settings 1080p 50 fps comes in around 8 MB for ~100 s.

Re-encode at 720p (still oversampled for the source pixel art) with a
moderately aggressive CRF:

```sh
ffmpeg -y -i ~/Movies/raw-recording.mov \
  -vf "scale=1280:720:flags=lanczos" \
  -c:v libx264 -preset slow -crf 30 \
  -c:a aac -b:a 64k \
  -movflags +faststart \
  media/atic-atac-demo.mp4
```

Targets:
- **CRF 30** is a good default for pixel art at 720p; usually lands the
  output around 3-4 MB for a 100 s capture.
- **`+faststart`** moves the moov atom to the start so the GitHub
  player can begin playing without downloading the whole file.

If the result is still over ~5 MB, drop the resolution to 540p, or
bump CRF to 32-34. Pixel art tolerates aggressive CRF well.

## 4. Crop blank margins (optional)

If the OBS canvas had blank space on any side, autodetect and crop:

```sh
# Sample a 10 s segment to find the content rectangle.
ffmpeg -i ~/Movies/raw-recording.mov \
  -vf "cropdetect=24:2:0" -ss 30 -t 10 -f null - 2>&1 \
  | grep -i "crop=" | tail -3
```

That prints something like `crop=1524:1080:12:0` — pass the same string
verbatim to the encode pipeline as the first filter:

```sh
ffmpeg -y -i ~/Movies/raw-recording.mov \
  -vf "crop=1524:1080:12:0,scale=-2:720:flags=lanczos" \
  -c:v libx264 -preset slow -crf 30 \
  -c:a aac -b:a 64k \
  -movflags +faststart \
  media/atic-atac-demo.mp4
```

Use `scale=-2:720` rather than a fixed width so the output stays at
the cropped aspect ratio (`-2` = "auto, must be even" — libx264
requires even dimensions).

## 5. Animated GIF preview

GIF compression is much weaker than H.264, so the GIF will be larger
than the equivalent MP4. Two-pass with palette generation is essential
for acceptable quality.

```sh
# Pass 1: build a palette tuned to the actual frames.
ffmpeg -y -i media/atic-atac-demo.mp4 \
  -vf "fps=50,scale=480:-1:flags=lanczos,palettegen=stats_mode=diff" \
  /tmp/palette.png

# Pass 2: render the GIF using that palette.
ffmpeg -y -i media/atic-atac-demo.mp4 -i /tmp/palette.png \
  -lavfi "fps=50,scale=480:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
  media/atic-atac-demo.gif

rm /tmp/palette.png
```

### The 50 fps gotcha

GIF frame delays are stored in centiseconds (1/100 s). 60 fps would
need a delay of 1.67 cs, which ffmpeg rounds to 1 — and most browsers
clamp delays under 2 cs to a default of 10, so a "60 fps GIF" plays at
**effective 10 fps** (very slow). Use **50 fps**: it gives a clean 2 cs
delay that all browsers honour, and it matches the game's native PAL
tick rate, so this is actually the authentic frame rate.

### Sizing

- **Width 480** is a good default — readable on Reddit/desktop, small
  enough that the GIF stays manageable.
- For a 100 s clip at 480×270 50 fps you can expect 12-17 MB. Well
  under GitHub's 100 MB hard limit but heavy on the README.
- Dropping to 25 fps roughly halves the size if needed:
  `fps=25` in both passes (browsers will clamp `25` to a 4 cs delay,
  no playback issue).

## 6. Embed in README

Drop the GIF straight under the title for instant impact:

```markdown
# Atic Atac — Go Replication

A faithful Go replication of...

![Atic Atac demo](media/atic-atac-demo.gif)
```

Linkable URLs for posting elsewhere:

- Inline player on GitHub: `https://github.com/<user>/<repo>/blob/main/media/atic-atac-demo.mp4`
- Direct download: `https://github.com/<user>/<repo>/raw/main/media/atic-atac-demo.mp4`
- Inline GIF (markdown image syntax in any markdown-rendering site):
  `https://github.com/<user>/<repo>/raw/main/media/atic-atac-demo.gif`

## 7. Commit and push

The MP4 + GIF together are usually 15-25 MB. Bumping the git HTTP
buffer once avoids a `HTTP 400` on the first push:

```sh
git config http.postBuffer 524288000
git add media/atic-atac-demo.mp4 media/atic-atac-demo.gif
git commit -m "Update demo media"
git push origin main
```

## Pitfalls and reminders

- **Don't commit the raw OBS recording** — it's an order of magnitude
  larger than the re-encode and bloats the repo forever. Keep raw
  recordings in `~/Movies/` outside the repo.
- **Inline MP4 size cap on GitHub is ~5 MB** for the blob viewer's
  HTML5 player. Above that the page shows "we can't show files that
  are this big right now" and the user has to download. Always
  re-encode targeting under 5 MB.
- **Integer-scale the game window** before recording. Pixel art at
  fractional scales (e.g. 3.7×) renders as blurry mush no matter what
  encoder you throw at it.
- **Test the audio track** with a 10 s clip before recording the full
  demo. Reshooting because the mic was muted is painful.
- **GIFs render inline in GitHub markdown but MP4s do not** — that's
  why the README embeds the GIF and the MP4 is reserved for the
  GitHub blob page link.
