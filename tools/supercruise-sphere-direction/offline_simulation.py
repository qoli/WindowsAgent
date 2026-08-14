#!/usr/bin/env python3
"""Offline full-resolution sphere detection and escape-direction simulation."""

from __future__ import annotations

import argparse
import json
import math
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import cv2
import numpy as np


ANGULAR_BINS = 72
RANSAC_ITERATIONS = 8000
MAX_RANSAC_POINTS = 6000
MIN_ANGULAR_BINS = 24
MIN_RANSAC_INLIERS = 80
DEFAULT_CLEARANCE_MARGIN_PIXELS = 32.0


@dataclass(frozen=True)
class CircleFit:
    center_x: float
    center_y: float
    radius: float
    inlier_count: int
    contour_point_count: int
    occupied_angular_bins: int
    median_residual_pixels: float
    score: float

    @property
    def angular_coverage_permille(self) -> int:
        return round(self.occupied_angular_bins * 1000 / ANGULAR_BINS)

    @property
    def inlier_ratio_permille(self) -> int:
        return round(self.inlier_count * 1000 / max(1, self.contour_point_count))


def circle_from_three(points: np.ndarray) -> tuple[float, float, float] | None:
    (x1, y1), (x2, y2), (x3, y3) = points.astype(float)
    denominator = 2.0 * (
        x1 * (y2 - y3) + x2 * (y3 - y1) + x3 * (y1 - y2)
    )
    if abs(denominator) < 1e-6:
        return None
    center_x = (
        (x1 * x1 + y1 * y1) * (y2 - y3)
        + (x2 * x2 + y2 * y2) * (y3 - y1)
        + (x3 * x3 + y3 * y3) * (y1 - y2)
    ) / denominator
    center_y = (
        (x1 * x1 + y1 * y1) * (x3 - x2)
        + (x2 * x2 + y2 * y2) * (x1 - x3)
        + (x3 * x3 + y3 * y3) * (x2 - x1)
    ) / denominator
    radius = math.hypot(x1 - center_x, y1 - center_y)
    return center_x, center_y, radius


def occupied_angular_bins(
    points: np.ndarray, center_x: float, center_y: float, inliers: np.ndarray
) -> int:
    angles = np.arctan2(
        points[inliers, 1] - center_y, points[inliers, 0] - center_x
    )
    bins = ((angles + np.pi) * ANGULAR_BINS / (2.0 * np.pi)).astype(int) % ANGULAR_BINS
    return int(len(np.unique(bins)))


def algebraic_circle_fit(points: np.ndarray) -> tuple[float, float, float]:
    matrix = np.c_[2.0 * points[:, 0], 2.0 * points[:, 1], np.ones(len(points))]
    values = points[:, 0] ** 2 + points[:, 1] ** 2
    center_x, center_y, constant = np.linalg.lstsq(matrix, values, rcond=None)[0]
    radius = math.sqrt(max(0.0, constant + center_x * center_x + center_y * center_y))
    return float(center_x), float(center_y), radius


def robust_circle_fit(
    contour_points: np.ndarray,
    image_width: int,
    image_height: int,
    seed: int,
) -> CircleFit | None:
    original_count = len(contour_points)
    rng = np.random.default_rng(seed)
    points = contour_points.astype(float)
    if len(points) > MAX_RANSAC_POINTS:
        indexes = rng.choice(len(points), MAX_RANSAC_POINTS, replace=False)
        points = points[indexes]

    minimum_dimension = min(image_width, image_height)
    minimum_radius = minimum_dimension * 0.06
    maximum_radius = minimum_dimension * 0.45
    residual_limit = max(3.0, minimum_dimension * 0.0025)
    best: tuple[float, int, int, tuple[float, float, float], np.ndarray] | None = None

    for _ in range(RANSAC_ITERATIONS):
        candidate = circle_from_three(points[rng.choice(len(points), 3, replace=False)])
        if candidate is None:
            continue
        center_x, center_y, radius = candidate
        if not minimum_radius <= radius <= maximum_radius:
            continue
        if not (-radius <= center_x <= image_width + radius):
            continue
        if not (-radius <= center_y <= image_height + radius):
            continue
        residuals = np.abs(
            np.hypot(points[:, 0] - center_x, points[:, 1] - center_y) - radius
        )
        inliers = residuals < residual_limit
        inlier_count = int(np.count_nonzero(inliers))
        if inlier_count < MIN_RANSAC_INLIERS:
            continue
        angular_bins = occupied_angular_bins(points, center_x, center_y, inliers)
        if angular_bins < MIN_ANGULAR_BINS:
            continue
        angular_fraction = angular_bins / ANGULAR_BINS
        score = inlier_count * angular_fraction * angular_fraction
        if best is None or score > best[0]:
            best = (score, inlier_count, angular_bins, candidate, inliers)

    if best is None:
        return None

    _, _, _, _, inliers = best
    center_x, center_y, radius = algebraic_circle_fit(points[inliers])
    # Re-select and refine once so the final result is measured against the
    # refined full-resolution circle rather than the three-point proposal.
    residuals = np.abs(
        np.hypot(points[:, 0] - center_x, points[:, 1] - center_y) - radius
    )
    inliers = residuals < residual_limit
    center_x, center_y, radius = algebraic_circle_fit(points[inliers])
    residuals = np.abs(
        np.hypot(points[:, 0] - center_x, points[:, 1] - center_y) - radius
    )
    inliers = residuals < residual_limit
    inlier_count = int(np.count_nonzero(inliers))
    angular_bins = occupied_angular_bins(points, center_x, center_y, inliers)
    median_residual = float(np.median(residuals[inliers]))
    angular_fraction = angular_bins / ANGULAR_BINS
    score = inlier_count * angular_fraction * angular_fraction
    return CircleFit(
        center_x=center_x,
        center_y=center_y,
        radius=radius,
        inlier_count=inlier_count,
        contour_point_count=min(original_count, MAX_RANSAC_POINTS),
        occupied_angular_bins=angular_bins,
        median_residual_pixels=median_residual,
        score=score,
    )


def threshold_full_resolution(image: np.ndarray) -> tuple[np.ndarray, float, dict[str, float]]:
    gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
    blurred = cv2.GaussianBlur(gray, (9, 9), 2.0)
    threshold, binary = cv2.threshold(
        blurred, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU
    )
    black_count = int(np.count_nonzero(binary == 0))
    white_count = int(binary.size - black_count)
    total = max(1, binary.size)
    metrics = {
        "blackPermille": round(black_count * 1000 / total),
        "whitePermille": round(white_count * 1000 / total),
    }
    return binary, float(threshold), metrics


def sphere_candidates(
    binary: np.ndarray, image_width: int, image_height: int, seed: int
) -> list[CircleFit]:
    contours, _ = cv2.findContours(binary, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_NONE)
    minimum_area = image_width * image_height * 0.005
    fits: list[CircleFit] = []
    for index, contour in enumerate(contours):
        if cv2.contourArea(contour) < minimum_area:
            continue
        fit = robust_circle_fit(
            contour[:, 0, :], image_width, image_height, seed + index * 7919
        )
        if fit is not None:
            fits.append(fit)
    return sorted(fits, key=lambda fit: fit.score, reverse=True)


def direction_name(vector_x: float, vector_y: float) -> str | None:
    absolute_x = abs(vector_x)
    absolute_y = abs(vector_y)
    if absolute_x < 1.0 and absolute_y < 1.0:
        return None
    diagonal = (
        absolute_x >= 32.0
        and absolute_y >= 32.0
        and absolute_x <= absolute_y * 3.0
        and absolute_y <= absolute_x * 3.0
    )
    horizontal = "YAW_RIGHT" if vector_x > 0 else "YAW_LEFT"
    vertical = "PITCH_DOWN" if vector_y > 0 else "PITCH_UP"
    if diagonal:
        return vertical + "_" + horizontal
    if absolute_x >= absolute_y:
        return horizontal
    return vertical


def direction_result(
    fit: CircleFit,
    image_width: int,
    image_height: int,
    clearance_margin: float,
) -> dict[str, Any]:
    screen_x = image_width / 2.0
    screen_y = image_height / 2.0
    # The escape vector points from the projected body's centre toward the
    # current boresight. Continuing in that direction moves away from the body.
    escape_x = screen_x - fit.center_x
    escape_y = screen_y - fit.center_y
    center_distance = math.hypot(escape_x, escape_y)
    signed_clearance = center_distance - fit.radius
    if center_distance < 1.0:
        return {
            "state": "UNKNOWN",
            "control": None,
            "reason": "SCREEN_CENTER_COINCIDENT_WITH_SPHERE_CENTER",
            "escapeVectorX": escape_x,
            "escapeVectorY": escape_y,
            "screenCenterDistancePixels": center_distance,
            "signedLimbClearancePixels": signed_clearance,
            "requiredTravelPixels": None,
        }
    if signed_clearance >= clearance_margin:
        return {
            "state": "CLEAR",
            "control": None,
            "reason": "SCREEN_CENTER_ALREADY_OUTSIDE_SPHERE_WITH_MARGIN",
            "escapeVectorX": escape_x,
            "escapeVectorY": escape_y,
            "screenCenterDistancePixels": center_distance,
            "signedLimbClearancePixels": signed_clearance,
            "requiredTravelPixels": 0.0,
        }
    required_travel = clearance_margin - signed_clearance
    return {
        "state": "READY",
        "control": direction_name(escape_x, escape_y),
        "reason": "MOVE_AWAY_FROM_FULL_RESOLUTION_SPHERE_CENTER",
        "escapeVectorX": escape_x,
        "escapeVectorY": escape_y,
        "screenCenterDistancePixels": center_distance,
        "signedLimbClearancePixels": signed_clearance,
        "requiredTravelPixels": required_travel,
    }


def simulate(
    image_path: Path, clearance_margin: float, seed: int
) -> tuple[dict[str, Any], np.ndarray, np.ndarray]:
    image = cv2.imread(str(image_path), cv2.IMREAD_COLOR)
    if image is None:
        raise RuntimeError(f"OpenCV could not decode {image_path}")
    height, width = image.shape[:2]
    binary, threshold, threshold_metrics = threshold_full_resolution(image)
    fits = sphere_candidates(binary, width, height, seed)
    report: dict[str, Any] = {
        "schemaVersion": 1,
        "mode": "OFFLINE_FULL_RESOLUTION_SIMULATION",
        "source": {
            "path": str(image_path.resolve()),
            "width": width,
            "height": height,
        },
        "enhancement": {
            "algorithm": "GAUSSIAN_9X9_SIGMA_2_THEN_OTSU",
            "threshold": threshold,
            **threshold_metrics,
        },
        "sphere": None,
        "direction": {
            "state": "UNKNOWN",
            "control": None,
            "reason": "FULL_RESOLUTION_SPHERE_NOT_DETECTED",
        },
    }
    if not fits:
        return report, image, binary
    fit = fits[0]
    confidence = round(
        min(1000.0, fit.angular_coverage_permille * 0.65 + fit.inlier_ratio_permille * 0.35)
    )
    report["sphere"] = {
        "state": "DETECTED",
        "centerX": fit.center_x,
        "centerY": fit.center_y,
        "radiusPixels": fit.radius,
        "diameterPixels": fit.radius * 2.0,
        "occupiedAngularBins": fit.occupied_angular_bins,
        "angularCoveragePermille": fit.angular_coverage_permille,
        "inlierCount": fit.inlier_count,
        "contourPointCount": fit.contour_point_count,
        "inlierRatioPermille": fit.inlier_ratio_permille,
        "medianResidualPixels": fit.median_residual_pixels,
        "confidencePermille": confidence,
        "candidateCount": len(fits),
    }
    report["direction"] = direction_result(
        fit, width, height, clearance_margin
    )
    return report, image, binary


def put_lines(
    image: np.ndarray,
    lines: list[str],
    origin: tuple[int, int],
    scale: float,
    color: tuple[int, int, int] = (245, 245, 245),
) -> None:
    x, y = origin
    step = round(34 * scale / 0.7)
    for line in lines:
        cv2.putText(
            image,
            line,
            (x, y),
            cv2.FONT_HERSHEY_SIMPLEX,
            scale,
            color,
            2,
            cv2.LINE_AA,
        )
        y += step


def annotated_image(image: np.ndarray, report: dict[str, Any]) -> np.ndarray:
    annotated = image.copy()
    height, width = annotated.shape[:2]
    screen_center = (round(width / 2.0), round(height / 2.0))
    cv2.drawMarker(
        annotated, screen_center, (255, 255, 255), cv2.MARKER_CROSS, 48, 4
    )
    sphere = report["sphere"]
    if sphere is None:
        put_lines(annotated, ["SPHERE: UNKNOWN"], (50, 70), 1.0, (0, 0, 255))
        return annotated
    center = (round(sphere["centerX"]), round(sphere["centerY"]))
    radius = round(sphere["radiusPixels"])
    cv2.circle(annotated, center, radius, (0, 255, 255), 5, cv2.LINE_AA)
    cv2.drawMarker(annotated, center, (0, 255, 255), cv2.MARKER_CROSS, 54, 4)

    direction = report["direction"]
    if direction["state"] == "READY":
        vector_x = direction["escapeVectorX"]
        vector_y = direction["escapeVectorY"]
        length = math.hypot(vector_x, vector_y)
        arrow_length = max(240.0, direction["requiredTravelPixels"])
        endpoint = (
            round(screen_center[0] + vector_x * arrow_length / length),
            round(screen_center[1] + vector_y * arrow_length / length),
        )
        cv2.arrowedLine(
            annotated,
            screen_center,
            endpoint,
            (0, 255, 0),
            8,
            cv2.LINE_AA,
            tipLength=0.08,
        )
    cv2.rectangle(annotated, (24, 20), (1480, 205), (8, 8, 8), -1)
    put_lines(
        annotated,
        [
            f"OTSU THRESHOLD: {report['enhancement']['threshold']:.0f}",
            f"SPHERE CENTER: ({sphere['centerX']:.1f}, {sphere['centerY']:.1f})  R={sphere['radiusPixels']:.1f}",
            f"FIT: {sphere['occupiedAngularBins']}/72 BINS  RESIDUAL={sphere['medianResidualPixels']:.2f}px  CONF={sphere['confidencePermille']}/1000",
            f"DIRECTION: {direction['state']}  {direction.get('control') or direction['reason']}",
        ],
        (50, 62),
        0.76,
    )
    return annotated


def preview_image(
    annotated: np.ndarray, binary: np.ndarray, report: dict[str, Any]
) -> np.ndarray:
    height, width = annotated.shape[:2]
    panel_width = 960
    panel_height = round(height * panel_width / width)
    left = cv2.resize(annotated, (panel_width, panel_height), interpolation=cv2.INTER_AREA)
    threshold_bgr = cv2.cvtColor(binary, cv2.COLOR_GRAY2BGR)
    right = cv2.resize(
        threshold_bgr, (panel_width, panel_height), interpolation=cv2.INTER_AREA
    )
    canvas = np.full((panel_height + 185, 1920, 3), 18, dtype=np.uint8)
    canvas[:panel_height, :panel_width] = left
    canvas[:panel_height, panel_width:] = right
    cv2.putText(canvas, "FULL-RES CIRCLE FIT", (24, panel_height + 38), cv2.FONT_HERSHEY_SIMPLEX, 0.8, (0, 255, 255), 2, cv2.LINE_AA)
    cv2.putText(canvas, "MAX BETWEEN-CLASS CONTRAST (OTSU)", (985, panel_height + 38), cv2.FONT_HERSHEY_SIMPLEX, 0.8, (245, 245, 245), 2, cv2.LINE_AA)
    sphere = report["sphere"]
    direction = report["direction"]
    if sphere is not None:
        lines = [
            f"Native {report['source']['width']}x{report['source']['height']} | center=({sphere['centerX']:.1f},{sphere['centerY']:.1f}) | radius={sphere['radiusPixels']:.1f}px",
            f"screen center=({report['source']['width']/2:.0f},{report['source']['height']/2:.0f}) | suggested={direction.get('control') or direction['state']} | limb clearance={direction['signedLimbClearancePixels']:.1f}px",
        ]
    else:
        lines = ["No sphere was accepted."]
    put_lines(canvas, lines, (24, panel_height + 90), 0.66)
    return canvas


def write_outputs(
    output_dir: Path,
    image_path: Path,
    report: dict[str, Any],
    image: np.ndarray,
    binary: np.ndarray,
) -> dict[str, str]:
    output_dir.mkdir(parents=True, exist_ok=True)
    stem = image_path.stem
    json_path = output_dir / f"{stem}-sphere-direction.json"
    threshold_path = output_dir / f"{stem}-threshold.png"
    annotated_path = output_dir / f"{stem}-annotated.png"
    preview_path = output_dir / f"{stem}-preview.png"
    annotated = annotated_image(image, report)
    preview = preview_image(annotated, binary, report)
    json_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if not cv2.imwrite(str(threshold_path), binary):
        raise RuntimeError(f"could not write {threshold_path}")
    if not cv2.imwrite(str(annotated_path), annotated):
        raise RuntimeError(f"could not write {annotated_path}")
    if not cv2.imwrite(str(preview_path), preview):
        raise RuntimeError(f"could not write {preview_path}")
    return {
        "json": str(json_path.resolve()),
        "threshold": str(threshold_path.resolve()),
        "annotated": str(annotated_path.resolve()),
        "preview": str(preview_path.resolve()),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image", type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument(
        "--clearance-margin-pixels",
        type=float,
        default=DEFAULT_CLEARANCE_MARGIN_PIXELS,
    )
    parser.add_argument("--seed", type=int, default=20260814)
    args = parser.parse_args()
    if args.clearance_margin_pixels < 0:
        parser.error("--clearance-margin-pixels must be non-negative")
    report, image, binary = simulate(
        args.image, args.clearance_margin_pixels, args.seed
    )
    outputs = write_outputs(
        args.output_dir, args.image, report, image, binary
    )
    print(json.dumps({"outputs": outputs, "result": report}))
    return 0 if report["sphere"] is not None else 2


if __name__ == "__main__":
    raise SystemExit(main())
