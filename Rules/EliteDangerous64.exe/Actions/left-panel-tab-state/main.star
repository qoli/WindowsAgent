SAMPLE_SIZE = 4
SELECTED_MINIMUM = 0.75
INACTIVE_MAXIMUM = 0.25
ABSENT_MAXIMUM = 0.25
OUTPUT_SCALE = 10000.0

TAB_POINTS = [
    {"name": "SYSTEM", "x": 328, "y": 295},
    {"name": "NAVIGATION", "x": 475, "y": 302},
    {"name": "TRANSACTIONS", "x": 720, "y": 298},
    {"name": "CONTACTS", "x": 929, "y": 296},
]

def is_highlight(red, green, blue):
    return red >= 180 and green >= 70 and green <= 220 and blue <= 90 and red >= green + 45

def read_tab_sample(point):
    sample = observer.screen.read_region(x=point["x"], y=point["y"], w=SAMPLE_SIZE, h=SAMPLE_SIZE, sampling="reference")
    if sample["sampling"] != "reference":
        return job.fail(code="LEFT_PANEL_TAB_EVIDENCE_INVALID", message=point["name"] + " tab sampling is not reference")
    image = sample["image"]
    if image["width"] != SAMPLE_SIZE or image["height"] != SAMPLE_SIZE or image["encoding"] != "rgb24-packed":
        return job.fail(code="LEFT_PANEL_TAB_EVIDENCE_INVALID", message=point["name"] + " tab image shape or encoding is invalid")
    pixels = image["pixels"]
    if len(pixels) != SAMPLE_SIZE * SAMPLE_SIZE:
        return job.fail(code="LEFT_PANEL_TAB_EVIDENCE_INVALID", message=point["name"] + " tab pixel count is incomplete")

    highlighted = 0
    for pixel in pixels:
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_highlight(red, green, blue):
            highlighted += 1
    ratio = math.round(float(highlighted) / float(len(pixels)) * OUTPUT_SCALE) / OUTPUT_SCALE
    return {
        "name": point["name"],
        "referenceRegion": {"x": point["x"], "y": point["y"], "w": SAMPLE_SIZE, "h": SAMPLE_SIZE},
        "physicalRegion": sample["physicalRegion"],
        "capturedAt": sample["frame"]["capturedAt"],
        "frameWidth": sample["frame"]["width"],
        "frameHeight": sample["frame"]["height"],
        "coordinateSpace": sample["coordinateSpace"],
        "highlightRatio": ratio,
    }

def main(ctx):
    samples = {}
    ratios = {}
    selected_tabs = []
    all_absent = True
    first = None
    last = None

    for point in TAB_POINTS:
        sample = read_tab_sample(point)
        if first == None:
            first = sample
        elif sample["frameWidth"] != first["frameWidth"] or sample["frameHeight"] != first["frameHeight"] or sample["coordinateSpace"] != first["coordinateSpace"]:
            return job.fail(code="LEFT_PANEL_TAB_EVIDENCE_INVALID", message="tab samples do not share one screen geometry")
        last = sample
        name = point["name"]
        ratio = sample["highlightRatio"]
        samples[name] = {
            "referenceRegion": sample["referenceRegion"],
            "physicalRegion": sample["physicalRegion"],
            "capturedAt": sample["capturedAt"],
            "highlightRatio": ratio,
        }
        ratios[name] = ratio
        if ratio > ABSENT_MAXIMUM:
            all_absent = False
        if ratio >= SELECTED_MINIMUM:
            selected_tabs.append(name)

    state = "UNKNOWN"
    reason = "TAB_HIGHLIGHT_AMBIGUOUS"
    if all_absent:
        state = "ABSENT"
        reason = "LEFT_PANEL_HEADER_ABSENT"
    elif len(selected_tabs) == 1:
        candidate = selected_tabs[0]
        inactive_valid = True
        for name in ratios:
            if name != candidate and ratios[name] > INACTIVE_MAXIMUM:
                inactive_valid = False
        if inactive_valid:
            state = candidate
            reason = candidate + "_POINT_CONFIRMED"
        else:
            reason = "MULTIPLE_TAB_POINTS_AMBIGUOUS"
    elif len(selected_tabs) > 1:
        reason = "MULTIPLE_SELECTED_TAB_POINTS_AMBIGUOUS"

    return {
        "schemaVersion": 3,
        "profile": {
            "width": first["frameWidth"],
            "height": first["frameHeight"],
            "capturedFrom": first["capturedAt"],
            "capturedThrough": last["capturedAt"],
        },
        "coordinateSpace": first["coordinateSpace"],
        "samples": samples,
        "activeTab": {
            "state": state,
            "highlightRatios": ratios,
            "selectedMinimum": SELECTED_MINIMUM,
            "inactiveMaximum": INACTIVE_MAXIMUM,
            "absentMaximum": ABSENT_MAXIMUM,
            "reason": reason,
        },
    }
