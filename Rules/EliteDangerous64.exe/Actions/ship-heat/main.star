def main(ctx):
    raw = action.call(id="elite-dangerous/ship-heat-text", inputs={})
    return action.call(id="elite-dangerous/ship-heat-classifier", inputs=raw)
