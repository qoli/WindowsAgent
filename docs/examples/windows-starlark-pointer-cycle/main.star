def main(ctx):
    clicks = []
    for point in [ctx.inputs["first"], ctx.inputs["second"]]:
        clicks.append(windows.pointer.click(
            expected_foreground=ctx.inputs["foreground"],
            x=point["x"],
            y=point["y"],
            hold_ms=40,
        ))
        task.activity(message="Pointer click completed", level="info")
        task.sleep(milliseconds=ctx.inputs["pauseMs"])
    return {"clicks": clicks}
