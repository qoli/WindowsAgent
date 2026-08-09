def main(ctx):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    task.sleep(milliseconds=1000)
    closed = action.call(id="elite-dangerous/close-left-panel", inputs={})
    return {
        "schemaVersion": 1,
        "backSent": True,
        "panelClosed": closed["closed"],
        "finalState": closed["finalState"],
    }
