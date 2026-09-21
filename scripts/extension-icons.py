#!/usr/bin/env python3
# The browser extension's icons, which are the TeaNode logo and nothing
# drawn for the occasion: the 16, 32 and 48 are the very images the
# dashboard ships as its favicon, and the 128 is docs/images/logo.svg
# rasterised.
#
#   python3 scripts/extension-icons.py
#
# The three small sizes need only Pillow. The 128 needs something that can
# draw an SVG -- rsvg-convert, inkscape or ImageMagick -- and is left alone
# when none is installed, since the one in the tree is already right.
import shutil
import subprocess
import sys
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
FAVICON = ROOT / 'web/public/favicon.ico'
LOGO = ROOT / 'docs/images/logo.svg'
ICONS = ROOT / 'web/extension/icons'


def from_favicon():
    for size in (16, 32, 48):
        image = Image.open(FAVICON)
        # Pick the frame of that size out of the icon file.
        image.size = (size, size)
        image.load()
        image.convert('RGBA').save(ICONS / f'icon{size}.png')
        print(f'icon{size}.png from {FAVICON.name}')


def from_logo():
    target = ICONS / 'icon128.png'
    for program, arguments in (
        ('rsvg-convert', ['-w', '128', '-h', '128', str(LOGO), '-o', str(target)]),
        ('inkscape', [str(LOGO), '--export-type=png', '-w', '128', '-h', '128', f'--export-filename={target}']),
        ('magick', [str(LOGO), '-resize', '128x128', str(target)]),
    ):
        if shutil.which(program):
            subprocess.run([program] + arguments, check=True)
            print(f'icon128.png from {LOGO.name} with {program}')
            return
    print('icon128.png left as it is: no SVG renderer found', file=sys.stderr)


if __name__ == '__main__':
    from_favicon()
    from_logo()
