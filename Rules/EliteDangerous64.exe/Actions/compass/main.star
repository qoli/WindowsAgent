REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 682
ROI_Y = 771
ROI_WIDTH = 96
ROI_HEIGHT = 96
CENTER_X = 48
CENTER_Y = 48
MIN_ORANGE_PIXELS = 150
MIN_CYAN_PIXELS = 4
CLEAR_HOLLOW_CENTER_MAX = 2
CLEAR_HOLLOW_MIN_WIDTH = 4
CLEAR_HOLLOW_MIN_HEIGHT = 6
CLEAR_SOLID_CENTER_MIN = 7
CENTER_ZONE_RADIUS = 4
OUTPUT_SCALE = 1000.0

def round_output(value):
    return math.round(value * OUTPUT_SCALE) / OUTPUT_SCALE

def is_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def is_cyan(red, green, blue):
    cyan_floor = min(green, blue)
    return (
        cyan_floor >= 100 and
        cyan_floor >= red + 12 and
        abs(green - blue) <= 48
    )

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
            code = "COMPASS_EVIDENCE_INVALID",
            message = "screen region sampling is not reference",
        )
    coordinate_space = sample["coordinateSpace"]
    if (
        coordinate_space["width"] != REFERENCE_WIDTH or
        coordinate_space["height"] != REFERENCE_HEIGHT or
        coordinate_space["fit"] != "centered-16:9"
    ):
        return job.fail(
            code = "COMPASS_EVIDENCE_INVALID",
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
            code = "COMPASS_EVIDENCE_INVALID",
            message = "screen region does not match the reviewed absolute coordinates",
        )
    image = sample["image"]
    if (
        image["width"] != ROI_WIDTH or
        image["height"] != ROI_HEIGHT or
        image["encoding"] != "rgb24-packed"
    ):
        return job.fail(
            code = "COMPASS_EVIDENCE_INVALID",
            message = "reference-sampled compass image shape or encoding is invalid",
        )
    pixels = image["pixels"]
    if len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(
            code = "COMPASS_EVIDENCE_INVALID",
            message = "screen region pixel count is incomplete",
        )

    orange_count = 0
    cyan_candidates = []
    candidate_cyan_count = 0
    for index in range(len(pixels)):
        pixel = pixels[index]
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_orange(red, green, blue):
            orange_count += 1
        candidate = is_cyan(red, green, blue)
        cyan_candidates.append(candidate)
        if candidate:
            candidate_cyan_count += 1

    # Preserve thin hollow rings while removing isolated color noise. This is
    # deliberately not a closing operation: filling the center would destroy
    # the front/rear topology that this Action must report.
    cyan_mask = []
    cyan_count = 0
    cyan_x_total = 0
    cyan_y_total = 0
    cyan_min_x = ROI_WIDTH
    cyan_min_y = ROI_HEIGHT
    cyan_max_x = -1
    cyan_max_y = -1
    for index in range(len(pixels)):
        pixel_x = index % ROI_WIDTH
        pixel_y = index // ROI_WIDTH
        neighbor_count = 0
        if cyan_candidates[index]:
            for neighbor_y in range(max(0, pixel_y - 1), min(ROI_HEIGHT, pixel_y + 2)):
                for neighbor_x in range(max(0, pixel_x - 1), min(ROI_WIDTH, pixel_x + 2)):
                    if cyan_candidates[neighbor_y * ROI_WIDTH + neighbor_x]:
                        neighbor_count += 1
        retained = neighbor_count >= 2
        cyan_mask.append(retained)
        if retained:
            cyan_count += 1
            cyan_x_total += pixel_x
            cyan_y_total += pixel_y
            cyan_min_x = min(cyan_min_x, pixel_x)
            cyan_min_y = min(cyan_min_y, pixel_y)
            cyan_max_x = max(cyan_max_x, pixel_x)
            cyan_max_y = max(cyan_max_y, pixel_y)

    if orange_count < MIN_ORANGE_PIXELS:
        return job.fail(
            code = "COMPASS_NOT_VISIBLE",
            message = "fixed compass region lacks the required orange HUD evidence",
        )

    target = {
        "detected": False,
        "candidateCyanPixelCount": candidate_cyan_count,
        "cyanPixelCount": cyan_count,
        "coreCyanPixelCount": 0,
        "centerCyanPixelCount": 0,
        "markerBounds": None,
        "presentation": "UNKNOWN",
        "hemisphere": "UNKNOWN",
        "referenceX": None,
        "referenceY": None,
        "offsetX": None,
        "offsetY": None,
        "screenAngleDegrees": None,
        "centerDistancePixels": None,
        "centerZone": {
            "shape": "circle",
            "radiusPixels": CENTER_ZONE_RADIUS,
            "inside": None,
        },
    }
    if cyan_count >= MIN_CYAN_PIXELS:
        marker_width = cyan_max_x - cyan_min_x + 1
        marker_height = cyan_max_y - cyan_min_y + 1
        topology_x = (cyan_min_x + cyan_max_x) // 2
        topology_y = (cyan_min_y + cyan_max_y) // 2
        core_cyan_count = 0
        center_cyan_count = 0
        for index in range(len(pixels)):
            pixel_x = index % ROI_WIDTH
            pixel_y = index // ROI_WIDTH
            if (
                abs(pixel_x - topology_x) <= 2 and
                abs(pixel_y - topology_y) <= 2 and
                cyan_mask[index]
            ):
                core_cyan_count += 1
            if (
                abs(pixel_x - topology_x) <= 1 and
                abs(pixel_y - topology_y) <= 1 and
                cyan_mask[index]
            ):
                center_cyan_count += 1
        presentation = "UNKNOWN"
        hemisphere = "UNKNOWN"
        if center_cyan_count >= CLEAR_SOLID_CENTER_MIN:
            presentation = "SOLID"
            hemisphere = "FRONT"
        elif (
            center_cyan_count <= CLEAR_HOLLOW_CENTER_MAX and
            marker_width >= CLEAR_HOLLOW_MIN_WIDTH and
            marker_height >= CLEAR_HOLLOW_MIN_HEIGHT
        ):
            presentation = "HOLLOW"
            hemisphere = "REAR"
        local_x = cyan_x_total // cyan_count
        local_y = cyan_y_total // cyan_count
        offset_x = local_x - CENTER_X
        offset_y = local_y - CENTER_Y
        distance = math.hypot(offset_x, offset_y)
        angle = None
        if distance != 0:
            angle = math.degrees(math.atan2(offset_x, -offset_y))
            if angle < 0:
                angle += 360
            angle = round_output(angle)
        target = {
            "detected": True,
            "candidateCyanPixelCount": candidate_cyan_count,
            "cyanPixelCount": cyan_count,
            "coreCyanPixelCount": core_cyan_count,
            "centerCyanPixelCount": center_cyan_count,
            "markerBounds": {
                "x": ROI_X + cyan_min_x,
                "y": ROI_Y + cyan_min_y,
                "w": marker_width,
                "h": marker_height,
                "centerX": ROI_X + topology_x,
                "centerY": ROI_Y + topology_y,
            },
            "presentation": presentation,
            "hemisphere": hemisphere,
            "referenceX": ROI_X + local_x,
            "referenceY": ROI_Y + local_y,
            "offsetX": offset_x,
            "offsetY": offset_y,
            "screenAngleDegrees": angle,
            "centerDistancePixels": round_output(distance),
            "centerZone": {
                "shape": "circle",
                "radiusPixels": CENTER_ZONE_RADIUS,
                "inside": distance <= CENTER_ZONE_RADIUS,
            },
        }

    return {
        "schemaVersion": 4,
        "profile": {
            "width": sample["frame"]["width"],
            "height": sample["frame"]["height"],
            "capturedAt": sample["frame"]["capturedAt"],
        },
        "coordinateSpace": coordinate_space,
        "region": {
            "x": ROI_X,
            "y": ROI_Y,
            "w": ROI_WIDTH,
            "h": ROI_HEIGHT,
            "centerX": ROI_X + CENTER_X,
            "centerY": ROI_Y + CENTER_Y,
        },
        "physicalRegion": sample["physicalRegion"],
        "compass": {
            "visible": True,
            "orangePixelCount": orange_count,
        },
        "target": target,
    }
