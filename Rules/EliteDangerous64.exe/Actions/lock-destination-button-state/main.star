ROI_X = 470
ROI_Y = 720
ROI_WIDTH = 140
ROI_HEIGHT = 60
FOCUSED_MINIMUM = 0.60
ABSENT_MAXIMUM = 0.12
OUTPUT_SCALE = 10000.0

def is_highlight(red, green, blue):
    return red >= 180 and green >= 70 and green <= 220 and blue <= 90 and red >= green + 45

def main(ctx):
    sample = observer.screen.read_region(x=ROI_X, y=ROI_Y, w=ROI_WIDTH, h=ROI_HEIGHT, sampling="reference")
    if sample["sampling"] != "reference":
        return job.fail(code="LOCK_DESTINATION_EVIDENCE_INVALID", message="Lock Destination sampling is not reference")
    image = sample["image"]
    if image["width"] != ROI_WIDTH or image["height"] != ROI_HEIGHT or image["encoding"] != "rgb24-packed":
        return job.fail(code="LOCK_DESTINATION_EVIDENCE_INVALID", message="Lock Destination image shape or encoding is invalid")
    pixels = image["pixels"]
    if len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(code="LOCK_DESTINATION_EVIDENCE_INVALID", message="Lock Destination pixel count is incomplete")

    highlighted = 0
    for pixel in pixels:
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_highlight(red, green, blue):
            highlighted += 1
    ratio = math.round(float(highlighted) / float(len(pixels)) * OUTPUT_SCALE) / OUTPUT_SCALE

    state = "UNKNOWN"
    focused = None
    reason = "HIGHLIGHT_RATIO_AMBIGUOUS"
    if ratio >= FOCUSED_MINIMUM:
        state = "FOCUSED"
        focused = True
        reason = "PRIMARY_DETAIL_ACTION_FILL_CONFIRMED"
    elif ratio <= ABSENT_MAXIMUM:
        state = "ABSENT"
        focused = False
        reason = "PRIMARY_DETAIL_ACTION_FILL_ABSENT"

    return {
        "schemaVersion": 1,
        "profile": {"width": sample["frame"]["width"], "height": sample["frame"]["height"], "capturedAt": sample["frame"]["capturedAt"]},
        "coordinateSpace": sample["coordinateSpace"],
        "region": {"x": ROI_X, "y": ROI_Y, "w": ROI_WIDTH, "h": ROI_HEIGHT},
        "physicalRegion": sample["physicalRegion"],
        "button": {
            "state": state,
            "focused": focused,
            "highlightRatio": ratio,
            "focusedMinimum": FOCUSED_MINIMUM,
            "absentMaximum": ABSENT_MAXIMUM,
            "reason": reason,
        },
    }
