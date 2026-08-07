def main(ctx):
    raw = action.call(id="elite-dangerous/ship-status-text-regions", inputs={})
    return action.call(id="elite-dangerous/ship-status-classifier", inputs=raw)
