ROI_X = 814
ROI_Y = 759
ROI_WIDTH = 264
ROI_HEIGHT = 36
MIN_SELECTED_LUMINANCE = 120.0
MIN_LUMINANCE_MARGIN = 25.0
OUTPUT_SCALE = 100.0

TILES = [
    {"name": "REFUEL", "index": 0, "x": 0, "w": 62},
    {"name": "REPAIR", "index": 1, "x": 67, "w": 63},
    {"name": "RESTOCK", "index": 2, "x": 134, "w": 63},
    {"name": "LAYER_SWITCH", "index": 3, "x": 202, "w": 62},
]

def luminance(pixel):
    red = pixel // 65536
    green = (pixel // 256) % 256
    blue = pixel % 256
    return float(77 * red + 150 * green + 29 * blue) / 256.0

def main(ctx):
    sample = observer.screen.read_region(
        x=ROI_X,
        y=ROI_Y,
        w=ROI_WIDTH,
        h=ROI_HEIGHT,
        sampling="reference",
    )
    if sample["sampling"] != "reference":
        return job.fail(code="STATION_SERVICE_FOCUS_EVIDENCE_INVALID", message="station service focus sampling is not reference")
    coordinate_space = sample["coordinateSpace"]
    if coordinate_space["width"] != 1920 or coordinate_space["height"] != 1080 or coordinate_space["fit"] != "centered-16:9":
        return job.fail(code="STATION_SERVICE_FOCUS_EVIDENCE_INVALID", message="station service focus coordinate space is not centered 1920x1080 reference")
    image = sample["image"]
    if image["width"] != ROI_WIDTH or image["height"] != ROI_HEIGHT or image["encoding"] != "rgb24-packed":
        return job.fail(code="STATION_SERVICE_FOCUS_EVIDENCE_INVALID", message="station service focus image shape or encoding is invalid")
    pixels = image["pixels"]
    if len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(code="STATION_SERVICE_FOCUS_EVIDENCE_INVALID", message="station service focus pixel count is incomplete")

    means = {}
    best_name = None
    best_index = None
    best_mean = -1.0
    second_mean = -1.0
    for tile in TILES:
        total = 0.0
        count = tile["w"] * ROI_HEIGHT
        for y in range(ROI_HEIGHT):
            row = y * ROI_WIDTH
            for x in range(tile["x"], tile["x"] + tile["w"]):
                total += luminance(pixels[row + x])
        mean = math.round(total / float(count) * OUTPUT_SCALE) / OUTPUT_SCALE
        means[tile["name"]] = mean
        if mean > best_mean:
            second_mean = best_mean
            best_mean = mean
            best_name = tile["name"]
            best_index = tile["index"]
        elif mean > second_mean:
            second_mean = mean

    margin = math.round((best_mean - second_mean) * OUTPUT_SCALE) / OUTPUT_SCALE
    state = "UNKNOWN"
    index = None
    reason = "HIGHLIGHT_LUMINANCE_TOO_LOW"
    if best_mean >= MIN_SELECTED_LUMINANCE:
        if margin >= MIN_LUMINANCE_MARGIN:
            state = best_name
            index = best_index
            reason = best_name + "_RELATIVE_LUMINANCE_CONFIRMED"
        else:
            reason = "HIGHLIGHT_LUMINANCE_AMBIGUOUS"

    return {
        "schemaVersion": 1,
        "focus": {
            "state": state,
            "index": index,
            "luminanceMeans": means,
            "selectedLuminance": best_mean,
            "runnerUpLuminance": second_mean,
            "luminanceMargin": margin,
            "minimumSelectedLuminance": MIN_SELECTED_LUMINANCE,
            "minimumLuminanceMargin": MIN_LUMINANCE_MARGIN,
            "reason": reason,
        },
        "evidence": {
            "referenceRegion": {"x": ROI_X, "y": ROI_Y, "w": ROI_WIDTH, "h": ROI_HEIGHT},
            "physicalRegion": sample["physicalRegion"],
            "capturedAt": sample["frame"]["capturedAt"],
            "coordinateSpace": coordinate_space,
        },
    }
