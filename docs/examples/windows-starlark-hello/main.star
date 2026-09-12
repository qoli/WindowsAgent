def main(ctx):
    task.activity(message = "Windows Starlark hello started", level = "info")
    return {
        "message": ctx.inputs["message"],
        "elapsedMilliseconds": task.elapsed_milliseconds(),
    }
