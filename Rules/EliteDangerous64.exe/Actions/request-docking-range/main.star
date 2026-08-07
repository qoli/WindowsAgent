def main(ctx):
    raw = action.call(id="elite-dangerous/request-docking-distance-text", inputs={})
    return action.call(id="elite-dangerous/request-docking-range-classifier", inputs=raw)
