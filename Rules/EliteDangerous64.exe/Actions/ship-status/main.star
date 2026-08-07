REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 1650
ROI_Y = 900
ROI_WIDTH = 270
ROI_HEIGHT = 180
MIN_COLOR_RUN_WIDTH = 8
MAX_COLOR_RUN_WIDTH = 22
MIN_BOX_X = 12
MAX_BOX_X = 85
MIN_BOX_EDGE_GAP = 10
MAX_BOX_EDGE_GAP = 16
MAX_BOX_CENTER_DELTA = 4
MAX_BOX_WIDTH_DELTA = 5
MIN_BOX_RUNS = 4
MIN_ROW_GAP = 16
MAX_ROW_GAP = 22
COLOR_ORANGE = "orange"
COLOR_CYAN = "cyan"

def is_orange(red, green, blue):
    return red >= 165 and green >= 45 and green <= 215 and blue <= 125 and red >= green + 30

def is_cyan(red, green, blue):
    return green >= 130 and blue >= 120 and red <= 190 and green >= red + 15 and blue >= red

def pixel_channels(pixel):
    return (
        pixel // 65536,
        (pixel // 256) % 256,
        pixel % 256,
    )

def append_color_run(runs, color, y, start_x, length):
    center_x = start_x + length // 2
    if (
        length >= MIN_COLOR_RUN_WIDTH and
        length <= MAX_COLOR_RUN_WIDTH and
        center_x >= MIN_BOX_X and
        center_x <= MAX_BOX_X
    ):
        runs.append({
            "color": color,
            "y": y,
            "startX": start_x,
            "width": length,
            "centerX": center_x,
        })

def build_box_candidates(runs):
    bands = []
    for run in runs:
        band_index = -1
        for index in range(len(bands)):
            band = bands[index]
            if (
                band["color"] == run["color"] and
                run["y"] >= band["endY"] and
                run["y"] <= band["endY"] + 1 and
                abs(run["centerX"] - band["centerX"]) <= MAX_BOX_CENTER_DELTA
            ):
                band_index = index
                break
        if band_index == -1:
            bands.append({
                "color": run["color"],
                "startY": run["y"],
                "endY": run["y"],
                "centerX": run["centerX"],
                "centerTotal": run["centerX"],
                "widthTotal": run["width"],
                "rowCount": 1,
            })
        elif run["y"] > bands[band_index]["endY"]:
            band = bands[band_index]
            band["endY"] = run["y"]
            band["centerTotal"] += run["centerX"]
            band["widthTotal"] += run["width"]
            band["rowCount"] += 1
            band["centerX"] = band["centerTotal"] // band["rowCount"]

    raw_candidates = []
    for band in bands:
        height = band["endY"] - band["startY"] + 1
        if (
            height >= MIN_BOX_EDGE_GAP and
            height <= MAX_BOX_EDGE_GAP and
            band["rowCount"] >= MIN_BOX_RUNS
        ):
            raw_candidates.append({
                "color": band["color"],
                "centerX": band["centerX"],
                "centerY": (band["startY"] + band["endY"]) // 2,
                "runCount": band["rowCount"],
            })

    for top in bands:
        for bottom in bands:
            edge_gap = bottom["endY"] - top["startY"]
            top_width = top["widthTotal"] // top["rowCount"]
            bottom_width = bottom["widthTotal"] // bottom["rowCount"]
            if (
                bottom["color"] == top["color"] and
                bottom["startY"] > top["endY"] + 1 and
                edge_gap >= MIN_BOX_EDGE_GAP and
                edge_gap <= MAX_BOX_EDGE_GAP and
                abs(bottom["centerX"] - top["centerX"]) <= MAX_BOX_CENTER_DELTA and
                abs(bottom_width - top_width) <= MAX_BOX_WIDTH_DELTA and
                top["rowCount"] + bottom["rowCount"] >= MIN_BOX_RUNS
            ):
                run_count = top["rowCount"] + bottom["rowCount"]
                raw_candidates.append({
                    "color": top["color"],
                    "centerX": (top["centerTotal"] + bottom["centerTotal"]) // run_count,
                    "centerY": (top["startY"] + bottom["endY"]) // 2,
                    "runCount": run_count,
                })

    candidates = []
    for candidate in raw_candidates:
        duplicate_index = -1
        for index in range(len(candidates)):
            existing = candidates[index]
            if (
                existing["color"] == candidate["color"] and
                abs(existing["centerX"] - candidate["centerX"]) <= 3 and
                abs(existing["centerY"] - candidate["centerY"]) <= 3
            ):
                duplicate_index = index
                break
        if duplicate_index == -1:
            candidates.append(candidate)
        elif candidate["runCount"] > candidates[duplicate_index]["runCount"]:
            candidates[duplicate_index] = candidate
    return candidates

def find_status_triplet(candidates):
    best = None
    best_score = 0
    for top in candidates:
        for middle in candidates:
            first_gap = middle["centerY"] - top["centerY"]
            if (
                abs(middle["centerX"] - top["centerX"]) <= MAX_BOX_CENTER_DELTA and
                first_gap >= MIN_ROW_GAP and
                first_gap <= MAX_ROW_GAP
            ):
                for bottom in candidates:
                    second_gap = bottom["centerY"] - middle["centerY"]
                    if (
                        abs(bottom["centerX"] - top["centerX"]) <= MAX_BOX_CENTER_DELTA and
                        second_gap >= MIN_ROW_GAP and
                        second_gap <= MAX_ROW_GAP
                    ):
                        score = top["runCount"] + middle["runCount"] + bottom["runCount"]
                        if score > best_score:
                            best_score = score
                            best = {
                                "top": top,
                                "middle": middle,
                                "bottom": bottom,
                                "score": score,
                            }
    return best

def build_status(candidate):
    if candidate == None:
        return {
            "state": "UNKNOWN",
            "on": None,
            "color": None,
            "referenceX": None,
            "referenceY": None,
        }
    if candidate["color"] == COLOR_CYAN:
        return {
            "state": "ON",
            "on": True,
            "color": COLOR_CYAN,
            "referenceX": ROI_X + candidate["centerX"],
            "referenceY": ROI_Y + candidate["centerY"],
        }
    return {
        "state": "OFF",
        "on": False,
        "color": COLOR_ORANGE,
        "referenceX": ROI_X + candidate["centerX"],
        "referenceY": ROI_Y + candidate["centerY"],
    }

def build_result(sample, orange_count, cyan_count, color_runs, box_candidates, triplet):
    panel_visible = triplet != None
    mass_lock = None
    landing_gear = None
    cargo_scoop = None
    triplet_score = 0
    if panel_visible:
        mass_lock = triplet["top"]
        landing_gear = triplet["middle"]
        cargo_scoop = triplet["bottom"]
        triplet_score = triplet["score"]
    return {
        "schemaVersion": 3,
        "profile": {
            "width": sample["frame"]["width"],
            "height": sample["frame"]["height"],
            "capturedAt": sample["frame"]["capturedAt"],
        },
        "coordinateSpace": sample["coordinateSpace"],
        "region": {
            "x": ROI_X,
            "y": ROI_Y,
            "w": ROI_WIDTH,
            "h": ROI_HEIGHT,
        },
        "physicalRegion": sample["physicalRegion"],
        "shipStatus": {
            "massLock": build_status(mass_lock),
            "landingGear": build_status(landing_gear),
            "cargoScoop": build_status(cargo_scoop),
        },
        "evidence": {
            "panelVisible": panel_visible,
            "orangePixelCount": orange_count,
            "cyanPixelCount": cyan_count,
            "colorRunCount": len(color_runs),
            "boxCandidateCount": len(box_candidates),
            "statusTripletDetected": panel_visible,
            "statusTripletScore": triplet_score,
        },
    }

def main(ctx):
    sample = observer.screen.read_region(
        x = ROI_X,
        y = ROI_Y,
        w = ROI_WIDTH,
        h = ROI_HEIGHT,
        sampling = "reference",
    )
    if sample["sampling"] != "reference":
        return job.fail(
            code = "SHIP_STATUS_EVIDENCE_INVALID",
            message = "screen region sampling is not reference",
        )
    coordinate_space = sample["coordinateSpace"]
    if (
        coordinate_space["width"] != REFERENCE_WIDTH or
        coordinate_space["height"] != REFERENCE_HEIGHT or
        coordinate_space["fit"] != "centered-16:9"
    ):
        return job.fail(
            code = "SHIP_STATUS_EVIDENCE_INVALID",
            message = "screen coordinate space is not the reviewed centered 1920x1080 reference",
        )
    region = sample["region"]
    if (
        region["x"] != ROI_X or
        region["y"] != ROI_Y or
        region["w"] != ROI_WIDTH or
        region["h"] != ROI_HEIGHT
    ):
        return job.fail(
            code = "SHIP_STATUS_EVIDENCE_INVALID",
            message = "screen region does not match the reviewed ship-status coordinates",
        )
    image = sample["image"]
    if (
        image["width"] != ROI_WIDTH or
        image["height"] != ROI_HEIGHT or
        image["encoding"] != "rgb24-packed"
    ):
        return job.fail(
            code = "SHIP_STATUS_EVIDENCE_INVALID",
            message = "reference-sampled ship-status image shape or encoding is invalid",
        )
    pixels = image["pixels"]
    if len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(
            code = "SHIP_STATUS_EVIDENCE_INVALID",
            message = "screen region pixel count is incomplete",
        )

    orange_count = 0
    cyan_count = 0
    color_runs = []
    for y in range(ROI_HEIGHT):
        orange_run_start = 0
        orange_run_length = 0
        cyan_run_start = 0
        cyan_run_length = 0
        for x in range(ROI_WIDTH):
            red, green, blue = pixel_channels(pixels[y * ROI_WIDTH + x])
            if is_orange(red, green, blue):
                orange_count += 1
                if orange_run_length == 0:
                    orange_run_start = x
                orange_run_length += 1
            else:
                append_color_run(color_runs, COLOR_ORANGE, y, orange_run_start, orange_run_length)
                orange_run_length = 0
            if is_cyan(red, green, blue):
                cyan_count += 1
                if cyan_run_length == 0:
                    cyan_run_start = x
                cyan_run_length += 1
            else:
                append_color_run(color_runs, COLOR_CYAN, y, cyan_run_start, cyan_run_length)
                cyan_run_length = 0
        append_color_run(color_runs, COLOR_ORANGE, y, orange_run_start, orange_run_length)
        append_color_run(color_runs, COLOR_CYAN, y, cyan_run_start, cyan_run_length)

    box_candidates = build_box_candidates(color_runs)
    triplet = find_status_triplet(box_candidates)
    return build_result(
        sample,
        orange_count,
        cyan_count,
        color_runs,
        box_candidates,
        triplet,
    )
