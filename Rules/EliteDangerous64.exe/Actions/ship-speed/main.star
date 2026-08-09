def main(ctx):
    raw = action.call(id="elite-dangerous/ship-speed-text", inputs={})
    glyph = action.call(id="elite-dangerous/ship-speed-zero-glyph", inputs={})
    return action.call(id="elite-dangerous/ship-speed-classifier", inputs={"ocr": raw, "glyph": glyph})
