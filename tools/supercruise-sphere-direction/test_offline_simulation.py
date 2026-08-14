import importlib.util
from pathlib import Path
import sys
import unittest

import cv2
import numpy as np


MODULE_PATH = Path(__file__).with_name("offline_simulation.py")
SPEC = importlib.util.spec_from_file_location("offline_simulation", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
simulation = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = simulation
SPEC.loader.exec_module(simulation)


class OfflineSimulationTests(unittest.TestCase):
    def test_full_resolution_fit_rejects_attached_line_and_recovers_sphere(self):
        image = np.zeros((900, 1600, 3), dtype=np.uint8)
        cv2.circle(image, (1040, 360), 230, (185, 185, 185), -1)
        cv2.line(image, (1030, 360), (1540, 700), (230, 230, 230), 5)
        cv2.line(image, (0, 760), (1600, 760), (220, 220, 220), 18)
        binary, threshold, _ = simulation.threshold_full_resolution(image)
        fits = simulation.sphere_candidates(binary, 1600, 900, seed=7)
        self.assertGreater(threshold, 0)
        self.assertTrue(fits)
        fit = fits[0]
        self.assertLess(abs(fit.center_x - 1040), 4)
        self.assertLess(abs(fit.center_y - 360), 4)
        self.assertLess(abs(fit.radius - 230), 4)
        self.assertGreaterEqual(fit.occupied_angular_bins, 60)

    def test_direction_points_away_from_upper_right_sphere(self):
        fit = simulation.CircleFit(1100, 420, 300, 500, 700, 68, 1.0, 450)
        result = simulation.direction_result(fit, 1920, 1080, 32)
        self.assertEqual(result["state"], "READY")
        self.assertEqual(result["control"], "PITCH_DOWN_YAW_LEFT")
        self.assertLess(result["escapeVectorX"], 0)
        self.assertGreater(result["escapeVectorY"], 0)

    def test_direction_reports_clear_when_boresight_has_margin(self):
        fit = simulation.CircleFit(300, 300, 120, 500, 700, 68, 1.0, 450)
        result = simulation.direction_result(fit, 1920, 1080, 32)
        self.assertEqual(result["state"], "CLEAR")
        self.assertIsNone(result["control"])
        self.assertEqual(result["requiredTravelPixels"], 0)


if __name__ == "__main__":
    unittest.main()
