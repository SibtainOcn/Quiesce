"""Build the Quiesce icon set from assets/quiesce-logo-source.png (coloured mark on black).

Crops to the mark, turns the black background transparent and writes:
  assets/quiesce-logo.png   512x512, for the README
  winres/icon/<size>.png    16..256 px, the exe icon (listed in winres/winres.json)
Then refresh the exe resources and rebuild:
  python tools/make-icons.py          (needs Pillow)
  go-winres make --arch amd64,386     (writes rsrc_windows_*.syso)
"""
from pathlib import Path
from PIL import Image

root = Path(__file__).resolve().parent.parent
src = Image.open(root / "assets/quiesce-logo-source.png").convert("RGB")

# Alpha from brightness: the background is black and the mark is saturated, so max(r,g,b) separates
# them. The dim neon glow (below ~50) is dropped so it does not leave a grey halo on light taskbars;
# colours are un-premultiplied so the anti-aliased edges stay vivid.
px = src.load()
w, h = src.size
out = Image.new("RGBA", src.size)
op = out.load()
for y in range(h):
    for x in range(w):
        r, g, b = px[x, y]
        a = max(0, min(255, (max(r, g, b) - 50) * 3))
        if a < 24:
            op[x, y] = (0, 0, 0, 0)
            continue
        k = 255 / a
        op[x, y] = (min(255, int(r * k)), min(255, int(g * k)), min(255, int(b * k)), a)

# Crop to the mark with a small margin, then square it.
l, t, r, b = out.getchannel("A").getbbox()
side = int(max(r - l, b - t) * 1.04)  # ~2% margin each side: the mark fills the icon
cx, cy = (l + r) // 2, (t + b) // 2
x0, y0 = cx - side // 2, cy - side // 2
sq = out.crop((x0, y0, x0 + side, y0 + side))

sq.resize((512, 512), Image.LANCZOS).save(root / "assets/quiesce-logo.png", optimize=True)
# one PNG per icon size, each downscaled from the full-resolution crop so small sizes stay sharp
(root / "winres/icon").mkdir(exist_ok=True)
for s in (16, 20, 24, 32, 40, 48, 64, 128, 256):
    sq.resize((s, s), Image.LANCZOS).save(root / f"winres/icon/{s}.png", optimize=True)
print("wrote assets/quiesce-logo.png, winres/icon/*.png")
