"""Regenerates the jam icon (design "1a": a guitar pick whose five fret-color
lanes converge toward its tip, on a dark rounded tile).

    python3 assets/icon/make_icon.py

Writes icon-{16..1024}.png next to this file and jam.ico (16-256 px).
Needs Pillow. Drawn at 2x and downsampled for clean edges.
"""
import os
from PIL import Image, ImageDraw, ImageFilter, ImageChops

OUT = os.path.dirname(os.path.abspath(__file__))
K = 2
W = 1024 * K


def P(v):
    return int(round(v * K))


def mk(fn):
    m = Image.new("L", (W, W), 0)
    fn(ImageDraw.Draw(m))
    return m


def mul(a, b):
    return ImageChops.multiply(a, b)


def vgrad(top, bot):
    g = Image.new("RGBA", (1, W))
    for y in range(W):
        t = y / (W - 1)
        g.putpixel((0, y), tuple(int(top[i] + (bot[i] - top[i]) * t) for i in range(4)))
    return g.resize((W, W))


def fill_grad(img, mask, top, bot):
    img.paste(vgrad(top, bot), (0, 0), mask)


def fill_solid(img, mask, c):
    layer = Image.new("RGBA", (W, W), c)
    layer.putalpha(mul(layer.getchannel("A"), mask))
    img.alpha_composite(layer)


def overlay_grad(img, mask, top, bot):
    layer = vgrad(top, bot)
    layer.putalpha(mul(layer.getchannel("A"), mask))
    img.alpha_composite(layer)


def shadow(img, mask, dy, blur, alpha):
    layer = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    layer.paste(Image.new("RGBA", (W, W), (0, 0, 0, alpha)), (0, P(dy)), mask)
    img.alpha_composite(layer.filter(ImageFilter.GaussianBlur(P(blur))))


def rpoly(pts, r):
    def f(d):
        d.polygon([(P(x), P(y)) for x, y in pts], fill=255)
        for i in range(len(pts)):
            a, b = pts[i], pts[(i + 1) % len(pts)]
            d.line([P(a[0]), P(a[1]), P(b[0]), P(b[1])], fill=255, width=P(2 * r))
            d.ellipse([P(a[0] - r), P(a[1] - r), P(a[0] + r), P(a[1] + r)], fill=255)
    return mk(f)


TILE = mk(lambda d: d.rounded_rectangle([P(32), P(32), P(992), P(992)], radius=P(200), fill=255))
FRET = [(52, 199, 105), (232, 64, 70), (246, 200, 52), (60, 130, 240), (245, 140, 40)]


def render():
    img = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    fill_grad(img, TILE, (30, 28, 38, 255), (8, 8, 12, 255))

    cx, cy, s = 512, 470, 1.0
    L = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    pts = [(cx + x * s, cy + y * s) for x, y in [(-182, -103), (182, -103), (0, 207)]]
    rr = 130 * s
    outer, inner = rpoly(pts, rr), rpoly(pts, rr - 10 * s)
    shadow(L, outer, 26 * s, 24, 190)
    vpx, vpy, ytop = cx, cy + 407 * s, cy - 313 * s
    xs = [cx - 332 * s + i * 132.8 * s for i in range(6)]
    for i in range(5):
        m = mk(lambda d: d.polygon([(P(vpx), P(vpy)), (P(xs[i]), P(ytop)), (P(xs[i + 1]), P(ytop))], fill=255))
        fill_solid(L, mul(m, outer), FRET[i] + (255,))
    ln = mk(lambda d: [d.line([P(vpx), P(vpy), P(x), P(ytop)], fill=255, width=P(5 * s)) for x in xs])
    fill_solid(L, mul(ln, outer), (14, 14, 20, 255))
    overlay_grad(L, outer, (0, 0, 0, 0), (0, 0, 0, 235))
    fill_solid(L, ImageChops.subtract(outer, inner), (255, 255, 255, 90))
    img.alpha_composite(L)

    img.putalpha(mul(img.getchannel("A"), TILE))
    rim = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    ImageDraw.Draw(rim).rounded_rectangle([P(32), P(32), P(992), P(992)], radius=P(200),
                                          outline=(255, 255, 255, 36), width=P(4))
    img.alpha_composite(rim)
    return img.resize((1024, 1024), Image.LANCZOS)


if __name__ == "__main__":
    master = render()
    for n in (16, 24, 32, 48, 64, 128, 256, 512, 1024):
        master.resize((n, n), Image.LANCZOS).save(os.path.join(OUT, f"icon-{n}.png"), optimize=True)
    master.save(os.path.join(OUT, "jam.ico"), sizes=[(n, n) for n in (16, 24, 32, 48, 64, 128, 256)])
