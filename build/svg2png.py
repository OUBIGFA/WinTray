# -*- coding: utf-8 -*-
"""Rasterize a single-path SVG into a PNG asset.

Kept in the repo so the icon assets under internal/branding/assets can be
regenerated from their SVG source instead of being opaque binaries.

Usage: python build/svg2png.py <input.svg> <output.png> <size> <#rrggbb>
"""

import re
import sys
import xml.etree.ElementTree as ET

from PIL import Image, ImageDraw

SUPERSAMPLE = 8
TOKEN = re.compile(r"[MmLlHhVvCcSsQqTtAaZz]|-?\d*\.?\d+(?:[eE][-+]?\d+)?")


def parse_path(d):
    """Flatten an SVG path into a list of closed subpaths (point lists)."""
    tokens = TOKEN.findall(d)
    subpaths, points = [], []
    cursor = start = (0.0, 0.0)
    command = None
    i = 0

    def flush():
        if len(points) > 2:
            subpaths.append(list(points))
        points.clear()

    while i < len(tokens):
        token = tokens[i]
        if re.match(r"[A-Za-z]", token):
            command = token
            i += 1
        elif command is None:
            raise ValueError("path data starts without a command")

        relative = command.islower()
        op = command.upper()

        def take(n):
            nonlocal i
            values = [float(v) for v in tokens[i:i + n]]
            i += n
            return values

        if op == "M":
            x, y = take(2)
            if relative:
                x, y = cursor[0] + x, cursor[1] + y
            flush()
            cursor = start = (x, y)
            points.append(cursor)
            command = "l" if relative else "L"
        elif op == "L":
            x, y = take(2)
            if relative:
                x, y = cursor[0] + x, cursor[1] + y
            cursor = (x, y)
            points.append(cursor)
        elif op == "H":
            (x,) = take(1)
            cursor = (cursor[0] + x if relative else x, cursor[1])
            points.append(cursor)
        elif op == "V":
            (y,) = take(1)
            cursor = (cursor[0], cursor[1] + y if relative else y)
            points.append(cursor)
        elif op == "C":
            x1, y1, x2, y2, x, y = take(6)
            if relative:
                x1, y1 = cursor[0] + x1, cursor[1] + y1
                x2, y2 = cursor[0] + x2, cursor[1] + y2
                x, y = cursor[0] + x, cursor[1] + y
            points.extend(flatten_cubic(cursor, (x1, y1), (x2, y2), (x, y)))
            cursor = (x, y)
        elif op == "Z":
            i += 0
            cursor = start
            flush()
            points.append(cursor)
        else:
            raise ValueError("unsupported path command: %s" % command)

    flush()
    return subpaths


def flatten_cubic(p0, p1, p2, p3, steps=24):
    out = []
    for step in range(1, steps + 1):
        t = step / steps
        u = 1.0 - t
        x = u * u * u * p0[0] + 3 * u * u * t * p1[0] + 3 * u * t * t * p2[0] + t * t * t * p3[0]
        y = u * u * u * p0[1] + 3 * u * u * t * p1[1] + 3 * u * t * t * p2[1] + t * t * t * p3[1]
        out.append((x, y))
    return out


def main():
    if len(sys.argv) != 5:
        print(__doc__.strip())
        return 1

    svg_path, png_path, size, color = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
    rgb = tuple(int(color.lstrip("#")[i:i + 2], 16) for i in (0, 2, 4))

    root = ET.parse(svg_path).getroot()
    view_box = [float(v) for v in root.get("viewBox", "0 0 24 24").split()]
    ns = "{http://www.w3.org/2000/svg}"
    paths = [el.get("d") for el in root.iter(ns + "path") if el.get("d")]
    if not paths:
        raise SystemExit("no <path> with a d attribute found")

    canvas = size * SUPERSAMPLE
    scale = canvas / max(view_box[2], view_box[3])
    image = Image.new("RGBA", (canvas, canvas), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)
    for d in paths:
        for subpath in parse_path(d):
            draw.polygon(
                [((x - view_box[0]) * scale, (y - view_box[1]) * scale) for x, y in subpath],
                fill=rgb + (255,),
            )

    image.resize((size, size), Image.LANCZOS).save(png_path)
    print("wrote %s (%dx%d)" % (png_path, size, size))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
