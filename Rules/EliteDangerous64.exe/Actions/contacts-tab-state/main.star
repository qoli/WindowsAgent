ROI_X = 890
ROI_Y = 286
ROI_WIDTH = 220
ROI_HEIGHT = 24
SELECTED_MINIMUM = 0.42
NOT_SELECTED_MINIMUM = 0.08
NOT_SELECTED_MAXIMUM = 0.30
ABSENT_MAXIMUM = 0.01
OUTPUT_SCALE = 10000.0

def is_highlight(red, green, blue):
    return red >= 180 and green >= 70 and green <= 220 and blue <= 90 and red >= green + 45

def main(ctx):
    sample = observer.screen.read_region(x=ROI_X, y=ROI_Y, w=ROI_WIDTH, h=ROI_HEIGHT, sampling="reference")
    if sample["sampling"] != "reference":
        return job.fail(code="CONTACTS_TAB_EVIDENCE_INVALID", message="Contacts tab sampling is not reference")
    image = sample["image"]
    if image["width"] != ROI_WIDTH or image["height"] != ROI_HEIGHT or image["encoding"] != "rgb24-packed":
        return job.fail(code="CONTACTS_TAB_EVIDENCE_INVALID", message="Contacts tab image shape or encoding is invalid")
    pixels = image["pixels"]
    if len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(code="CONTACTS_TAB_EVIDENCE_INVALID", message="Contacts tab pixel count is incomplete")

    highlighted = 0
    for pixel in pixels:
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_highlight(red, green, blue):
            highlighted += 1
    ratio = math.round(float(highlighted) / float(len(pixels)) * OUTPUT_SCALE) / OUTPUT_SCALE

    state = "UNKNOWN"
    selected = None
    reason = "HIGHLIGHT_RATIO_AMBIGUOUS"
    if ratio <= ABSENT_MAXIMUM:
        state = "ABSENT"
        reason = "CONTACTS_REGION_ABSENT"
    elif ratio >= SELECTED_MINIMUM:
        state = "SELECTED"
        selected = True
        reason = "CONTACTS_FILL_CONFIRMED"
    elif ratio >= NOT_SELECTED_MINIMUM and ratio <= NOT_SELECTED_MAXIMUM:
        state = "NOT_SELECTED"
        selected = False
        reason = "CONTACTS_FILL_NOT_SELECTED"

    return {
        "schemaVersion": 1,
        "profile": {"width": sample["frame"]["width"], "height": sample["frame"]["height"], "capturedAt": sample["frame"]["capturedAt"]},
        "coordinateSpace": sample["coordinateSpace"],
        "region": {"x": ROI_X, "y": ROI_Y, "w": ROI_WIDTH, "h": ROI_HEIGHT},
        "physicalRegion": sample["physicalRegion"],
        "contactsTab": {
            "state": state,
            "selected": selected,
            "highlightRatio": ratio,
            "selectedMinimum": SELECTED_MINIMUM,
            "notSelectedMinimum": NOT_SELECTED_MINIMUM,
            "notSelectedMaximum": NOT_SELECTED_MAXIMUM,
            "absentMaximum": ABSENT_MAXIMUM,
            "reason": reason,
        },
    }
