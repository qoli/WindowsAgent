def main(ctx):
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    flight = action.call(id="elite-dangerous/flight-status", inputs=raw)
    cockpit = action.call(id="elite-dangerous/cockpit-hud-presence", inputs={})
    flight_state = flight["flightStatus"]["state"]
    cockpit_state = cockpit["cockpitHud"]["state"]
    state = "COCKPIT_PRESENT" if cockpit_state == "PRESENT" else "COCKPIT_ABSENT"
    if flight_state == "FSD_CHARGING":
        state = "FSD_CHARGING"
    elif flight_state == "FSD_ALIGNMENT_REQUIRED":
        state = "ALIGNMENT_REQUIRED"
    return {
        "schemaVersion": 1,
        "hyperspaceState": {
            "state": state,
            "flightStatus": flight_state,
            "promptText": raw["text"],
            "cockpitHud": cockpit["cockpitHud"],
            "evidenceCapturedAt": cockpit["profile"]["capturedAt"],
        },
    }
