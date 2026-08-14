def main(ctx):
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    classified = raw["cascade"]["selectedDecision"]
    attempts = []
    for attempt in raw["cascade"]["attempts"]:
        attempts.append({
            "routeId": attempt["routeId"],
            "text": attempt["text"],
            "confidence": attempt["confidence"],
            "routeDecision": attempt["decision"]["routeDecision"],
            "timing": attempt["timing"],
        })
    return {
        "schemaVersion": 2,
        "flightStatus": classified["flightStatus"],
        "source": classified["source"],
        "decision": classified["decision"],
        "recognition": {
            "policy": raw["cascade"]["policy"],
            "selectedRoute": raw["cascade"]["selectedRoute"],
            "terminalReason": raw["cascade"]["terminalReason"],
            "attemptCount": raw["cascade"]["attemptCount"],
            "attempts": attempts,
            "gate": raw["cascade"]["gate"],
            "transitions": raw["cascade"]["transitions"],
            "evidence": raw["evidence"],
            "model": raw["model"],
            "timing": raw["timing"],
        },
    }
