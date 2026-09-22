def main(ctx):
    info = windows.fs.stat(path = ctx.inputs["path"])
    if info["isDir"]:
        task.fail(code = "EXPECTED_FILE", message = "The selected path is a directory")
    content = windows.fs.read(path = ctx.inputs["path"])
    task.activity(message = "Selected text file read successfully", level = "info")
    return {
        "verified": True,
        "characters": len(list(content.codepoints())),
        "elapsedMilliseconds": task.elapsed_milliseconds(),
    }
