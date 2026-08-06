FRAME_WIDTH = 3840
FRAME_HEIGHT = 2160
ROI_LEFT = 1340
ROI_TOP = 1560
ROI_WIDTH = 192
ROI_HEIGHT = 192
CENTER_X = 104
CENTER_Y = 87
MIN_ORANGE_PIXELS = 400
MIN_CYAN_PIXELS = 12
CENTER_TOLERANCE = 8

def is_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def is_cyan(red, green, blue):
    return green >= 150 and blue >= 140 and red <= 180 and green >= red + 20 and blue >= red + 5

def main(ctx):
    sample = observer.screen.read_region(
        expectedWidth = FRAME_WIDTH,
        expectedHeight = FRAME_HEIGHT,
        left = ROI_LEFT,
        top = ROI_TOP,
        width = ROI_WIDTH,
        height = ROI_HEIGHT,
    )
    if sample["encoding"] != "rgb24-packed":
        return job.fail(
            code = "COMPASS_EVIDENCE_INVALID",
            message = "screen region encoding is not rgb24-packed",
        )
    region = sample["region"]
    if (
        region["left"] != ROI_LEFT or
        region["top"] != ROI_TOP or
        region["width"] != ROI_WIDTH or
        region["height"] != ROI_HEIGHT
    ):
        return job.fail(
            code = "COMPASS_EVIDENCE_INVALID",
            message = "screen region does not match the reviewed absolute coordinates",
        )
    pixels = sample["pixels"]
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
        "screenX": None,
        "screenY": None,
        "offsetX": None,
        "offsetY": None,
        "centered": None,
    }
    if cyan_count >= MIN_CYAN_PIXELS:
        local_x = cyan_x_total // cyan_count
        local_y = cyan_y_total // cyan_count
        offset_x = local_x - CENTER_X
        offset_y = local_y - CENTER_Y
        target = {
            "detected": True,
            "cyanPixelCount": cyan_count,
            "screenX": ROI_LEFT + local_x,
            "screenY": ROI_TOP + local_y,
            "offsetX": offset_x,
            "offsetY": offset_y,
            "centered": abs(offset_x) <= CENTER_TOLERANCE and abs(offset_y) <= CENTER_TOLERANCE,
        }

    return {
        "schemaVersion": 1,
        "profile": {
            "width": FRAME_WIDTH,
            "height": FRAME_HEIGHT,
            "capturedAt": sample["frame"]["capturedAt"],
        },
        "region": {
            "left": ROI_LEFT,
            "top": ROI_TOP,
            "width": ROI_WIDTH,
            "height": ROI_HEIGHT,
            "centerX": ROI_LEFT + CENTER_X,
            "centerY": ROI_TOP + CENTER_Y,
        },
        "compass": {
            "visible": True,
            "orangePixelCount": orange_count,
        },
        "target": target,
    }
