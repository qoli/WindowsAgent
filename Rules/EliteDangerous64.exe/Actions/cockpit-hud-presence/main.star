REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 1280
ROI_Y = 790
ROI_WIDTH = 120
ROI_HEIGHT = 120
MIN_HUD_PIXELS = 250

def is_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def is_charge_cyan(red, green, blue):
    return red <= 130 and green >= 130 and blue >= 140 and green >= red + 40 and blue >= red + 40

def main(ctx):
    sample = observer.screen.read_region(
        x=ROI_X,
        y=ROI_Y,
        w=ROI_WIDTH,
        h=ROI_HEIGHT,
        sampling="reference",
    )
    coordinate_space = sample["coordinateSpace"]
    image = sample["image"]
    if sample["sampling"] != "reference" or coordinate_space["width"] != REFERENCE_WIDTH or coordinate_space["height"] != REFERENCE_HEIGHT or coordinate_space["fit"] != "centered-16:9":
        return job.fail(code="COCKPIT_HUD_EVIDENCE_INVALID", message="screen coordinate space is not the reviewed centered 1920x1080 reference")
    if image["width"] != ROI_WIDTH or image["height"] != ROI_HEIGHT or image["encoding"] != "rgb24-packed" or len(image["pixels"]) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(code="COCKPIT_HUD_EVIDENCE_INVALID", message="cockpit HUD image shape or encoding is invalid")
    orange_count = 0
    charge_cyan_count = 0
    for pixel in image["pixels"]:
        red = pixel // 65536
        green = (pixel // 256) % 256
        blue = pixel % 256
        if is_orange(red, green, blue):
            orange_count += 1
        elif is_charge_cyan(red, green, blue):
            charge_cyan_count += 1
    hud_count = orange_count + charge_cyan_count
    visible = hud_count >= MIN_HUD_PIXELS
    return {
        "schemaVersion": 1,
        "cockpitHud": {
            "state": "PRESENT" if visible else "ABSENT",
            "orangePixelCount": orange_count,
            "chargeCyanPixelCount": charge_cyan_count,
            "hudPixelCount": hud_count,
            "minimumHudPixels": MIN_HUD_PIXELS,
        },
        "profile": {
            "width": sample["frame"]["width"],
            "height": sample["frame"]["height"],
            "capturedAt": sample["frame"]["capturedAt"],
        },
        "region": {"x": ROI_X, "y": ROI_Y, "w": ROI_WIDTH, "h": ROI_HEIGHT},
    }
