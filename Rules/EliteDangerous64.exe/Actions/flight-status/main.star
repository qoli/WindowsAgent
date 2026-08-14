def main(ctx):
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    return action.call(id="elite-dangerous/flight-status-classifier", inputs=raw)
