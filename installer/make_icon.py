"""Generates icon.ico - the same motif the tray icon draws (sound bars).

A multi-size .ico (16..256) lands next to this file; go-winres turns it into the
.syso that bakes the icon into the executables at build time.

It also writes docs/icon.png, which is what the README shows: GitHub renders PNG
everywhere and .ico only sometimes.
"""
import os
from PIL import Image, ImageDraw

OUT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(OUT_DIR)
ICO = os.path.join(OUT_DIR, "icon.ico")
PNG = os.path.join(REPO, "docs", "icon.png")

# the background is the same green the tray uses for "all good"
GREEN = (0x2e, 0xa0, 0x43, 255)
WHITE = (255, 255, 255, 255)


def render(size):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    r = max(2, size // 6)
    # rounded square background
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=r, fill=GREEN)
    # three bars of differing height, as fractions of the size, same as the tray
    bars = [(0.22, 0.34, 0.66), (0.44, 0.19, 0.81), (0.66, 0.31, 0.69)]
    bw = size * 0.12
    for cx, top, bottom in bars:
        x = size * cx
        d.rounded_rectangle(
            [x, size * top, x + bw, size * bottom],
            radius=max(1, int(bw / 3)), fill=WHITE,
        )
    return img


sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]

# Save from the LARGEST render. Pillow builds an .ico by resizing the image it
# is given, and silently drops every requested size bigger than that image - so
# handing it the 16px render produces a 176-byte file holding one tiny icon,
# which then gets stretched into a blurry mess everywhere it is shown.
base = render(max(sizes))
base.save(ICO, format="ICO", sizes=[(s, s) for s in sizes])
print("icon.ico:", os.path.getsize(ICO), "bytes,", len(sizes), "sizes")

os.makedirs(os.path.dirname(PNG), exist_ok=True)
base.save(PNG, format="PNG")
print("icon.png:", os.path.getsize(PNG), "bytes")
