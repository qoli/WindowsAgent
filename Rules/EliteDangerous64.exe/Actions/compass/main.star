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
CENTER_ZONE_RADIUS = 4
OUTPUT_SCALE = 1000.0

def round_output(value):
    return math.round(value * OUTPUT_SCALE) / OUTPUT_SCALE

def is_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def is_cyan(red, green, blue):
    return green >= 150 and blue >= 140 and red <= 180 and green >= red + 20 and blue >= red + 5

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
    cyan_count = 0
    cyan_x_total = 0
    cyan_y_total = 0
    for index in range(len(pixels)):
        pixel = pixels[index]
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_orange(red, green, blue):
            orange_count += 1
        if is_cyan(red, green, blue):
            cyan_count += 1
            cyan_x_total += index % ROI_WIDTH
            cyan_y_total += index // ROI_WIDTH

    if orange_count < MIN_ORANGE_PIXELS:
        return job.fail(
            code = "COMPASS_NOT_VISIBLE",
            message = "fixed compass region lacks the required orange HUD evidence",
        )

    target = {
        "detected": False,
        "cyanPixelCount": cyan_count,
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
            "cyanPixelCount": cyan_count,
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
        "schemaVersion": 2,
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
