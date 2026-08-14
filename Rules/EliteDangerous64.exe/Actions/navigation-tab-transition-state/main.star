SAMPLE_SIZE = 4
SELECTED_MINIMUM = 0.75
OUTPUT_SCALE = 10000.0
NAVIGATION_POINT = {"x": 475, "y": 302}

def is_highlight(red, green, blue):
    return red >= 180 and green >= 70 and green <= 220 and blue <= 90 and red >= green + 45

def main(ctx):
    sample = observer.screen.read_region(
        x=NAVIGATION_POINT["x"],
        y=NAVIGATION_POINT["y"],
        w=SAMPLE_SIZE,
        h=SAMPLE_SIZE,
        sampling="reference",
    )
    if sample["sampling"] != "reference":
        return job.fail(code="NAVIGATION_TAB_TRANSITION_EVIDENCE_INVALID", message="Navigation tab sampling is not reference")
    image = sample["image"]
    if image["width"] != SAMPLE_SIZE or image["height"] != SAMPLE_SIZE or image["encoding"] != "rgb24-packed":
        return job.fail(code="NAVIGATION_TAB_TRANSITION_EVIDENCE_INVALID", message="Navigation tab image shape or encoding is invalid")
    pixels = image["pixels"]
    if len(pixels) != SAMPLE_SIZE * SAMPLE_SIZE:
        return job.fail(code="NAVIGATION_TAB_TRANSITION_EVIDENCE_INVALID", message="Navigation tab pixel count is incomplete")

    highlighted = 0
    for pixel in pixels:
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_highlight(red, green, blue):
            highlighted += 1
    ratio = math.round(float(highlighted) / float(len(pixels)) * OUTPUT_SCALE) / OUTPUT_SCALE
    confirmed = ratio >= SELECTED_MINIMUM
    return {
        "schemaVersion": 1,
        "state": "NAVIGATION" if confirmed else "NOT_CONFIRMED",
        "referenceRegion": {"x": NAVIGATION_POINT["x"], "y": NAVIGATION_POINT["y"], "w": SAMPLE_SIZE, "h": SAMPLE_SIZE},
        "physicalRegion": sample["physicalRegion"],
        "coordinateSpace": sample["coordinateSpace"],
        "capturedAt": sample["frame"]["capturedAt"],
        "highlightRatio": ratio,
        "selectedMinimum": SELECTED_MINIMUM,
        "reason": "NAVIGATION_POINT_CONFIRMED" if confirmed else "NAVIGATION_POINT_NOT_CONFIRMED",
    }
