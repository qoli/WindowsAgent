def main(ctx):
    raw = action.call(id="elite-dangerous/ship-speed-text", inputs={})
    return action.call(id="elite-dangerous/ship-speed-classifier", inputs=raw)
