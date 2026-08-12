def main(ctx):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    task.sleep(milliseconds=900)
    back_count = 1
    if ctx.inputs["dialogMayBeOpen"]:
        raw = action.call(id="elite-dangerous/commodity-market-header-text-regions", inputs={})
        market_present = False
        for region in raw["regions"]:
            normalized = ""
            upper = region["text"].upper()
            for index in range(len(upper)):
                character = upper[index]
                if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
                    normalized += character
            if normalized == "COMMODITIESMARKET":
                market_present = True
        if market_present:
            action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
            task.sleep(milliseconds=900)
            back_count += 1
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    task.sleep(milliseconds=900)
    back_count += 1
    return {"schemaVersion": 1, "backCount": back_count, "settleMs": back_count * 900}
