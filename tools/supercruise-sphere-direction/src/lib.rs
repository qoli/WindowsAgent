//! Bounded conventional CV for the Elite Dangerous LOS obstruction workflow.

use std::f64::consts::TAU;

const ANGULAR_BINS: usize = 72;
const RANSAC_ITERATIONS: usize = 6_000;
const MAX_BOUNDARY_POINTS: usize = 6_000;
const MIN_ANGULAR_BINS: usize = 18;
const MIN_INLIERS: usize = 24;

#[repr(C)]
#[derive(Clone, Copy, Debug, Default)]
pub struct SphereResult {
    pub state: u32,
    pub control: u32,
    pub center_x_milli: i32,
    pub center_y_milli: i32,
    pub radius_milli: u32,
    pub signed_clearance_milli: i32,
    pub confidence_permille: u32,
    pub occupied_angular_bins: u32,
    pub inlier_count: u32,
    pub boundary_point_count: u32,
    pub median_residual_milli: u32,
    pub otsu_threshold: u32,
    pub black_permille: u32,
    pub white_permille: u32,
    pub candidate_count: u32,
}
#[derive(Clone, Copy, Debug)]
struct Circle {
    x: f64,
    y: f64,
    radius: f64,
    inliers: usize,
    bins: usize,
    residual: f64,
    score: f64,
}

fn luminance(pixel: u32) -> u8 {
    let r = (pixel >> 16) & 0xff;
    let g = (pixel >> 8) & 0xff;
    let b = pixel & 0xff;
    ((77 * r + 150 * g + 29 * b) >> 8) as u8
}

fn blur_9x9(gray: &[u8], width: usize, height: usize) -> Vec<u8> {
    const K: [u32; 9] = [7, 17, 32, 46, 52, 46, 32, 17, 7];
    let mut horizontal = vec![0u32; gray.len()];
    for y in 0..height {
        for x in 0..width {
            let mut sum = 0u32;
            for (index, weight) in K.iter().enumerate() {
                let source_x = (x as isize + index as isize - 4).clamp(0, width as isize - 1) as usize;
                sum += *weight * gray[y * width + source_x] as u32;
            }
            horizontal[y * width + x] = sum;
        }
    }
    let mut output = vec![0u8; gray.len()];
    for y in 0..height {
        for x in 0..width {
            let mut sum = 0u64;
            for (index, weight) in K.iter().enumerate() {
                let source_y = (y as isize + index as isize - 4).clamp(0, height as isize - 1) as usize;
                sum += *weight as u64 * horizontal[source_y * width + x] as u64;
            }
            output[y * width + x] = ((sum + 32_768) / 65_536).min(255) as u8;
        }
    }
    output
}

fn otsu(values: &[u8]) -> u8 {
    let mut histogram = [0u64; 256];
    for value in values { histogram[*value as usize] += 1; }
    let total = values.len() as u64;
    let weighted_total: u64 = histogram.iter().enumerate().map(|(i, n)| i as u64 * n).sum();
    let mut background_count = 0u64;
    let mut background_sum = 0u64;
    let mut best_variance = -1.0f64;
    let mut best = 0u8;
    for threshold in 0..256 {
        background_count += histogram[threshold];
        if background_count == 0 { continue; }
        let foreground_count = total - background_count;
        if foreground_count == 0 { break; }
        background_sum += threshold as u64 * histogram[threshold];
        let background_mean = background_sum as f64 / background_count as f64;
        let foreground_mean = (weighted_total - background_sum) as f64 / foreground_count as f64;
        let difference = background_mean - foreground_mean;
        let variance = background_count as f64 * foreground_count as f64 * difference * difference;
        if variance > best_variance { best_variance = variance; best = threshold as u8; }
    }
    best
}

fn boundary_points(binary: &[bool], width: usize, height: usize) -> Vec<(f64, f64)> {
    let flight_height = height * 3 / 4;
    let mut points = Vec::new();
    for y in 1..flight_height.saturating_sub(1) {
        for x in 1..width.saturating_sub(1) {
            let index = y * width + x;
            if !binary[index] { continue; }
            if !binary[index - 1] || !binary[index + 1] || !binary[index - width] || !binary[index + width] {
                points.push((x as f64, y as f64));
            }
        }
    }
    if points.len() <= MAX_BOUNDARY_POINTS { return points; }
    let mut sampled = Vec::with_capacity(MAX_BOUNDARY_POINTS);
    for index in 0..MAX_BOUNDARY_POINTS {
        sampled.push(points[index * points.len() / MAX_BOUNDARY_POINTS]);
    }
    sampled
}

fn circle_from_three(a: (f64, f64), b: (f64, f64), c: (f64, f64)) -> Option<(f64, f64, f64)> {
    let denominator = 2.0 * (a.0 * (b.1 - c.1) + b.0 * (c.1 - a.1) + c.0 * (a.1 - b.1));
    if denominator.abs() < 1e-6 { return None; }
    let x = ((a.0*a.0+a.1*a.1)*(b.1-c.1) + (b.0*b.0+b.1*b.1)*(c.1-a.1) + (c.0*c.0+c.1*c.1)*(a.1-b.1)) / denominator;
    let y = ((a.0*a.0+a.1*a.1)*(c.0-b.0) + (b.0*b.0+b.1*b.1)*(a.0-c.0) + (c.0*c.0+c.1*c.1)*(b.0-a.0)) / denominator;
    Some((x, y, (a.0-x).hypot(a.1-y)))
}

fn next_random(state: &mut u64, limit: usize) -> usize {
    *state = state.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
    ((*state >> 32) as usize) % limit
}

fn fit_circle(points: &[(f64, f64)], width: usize, height: usize) -> (Option<Circle>, usize) {
    if points.len() < MIN_INLIERS { return (None, 0); }
    let minimum = width.min(height) as f64;
    let min_radius = minimum * 0.04;
    let max_radius = minimum * 0.50;
    let residual_limit = (minimum * 0.009).max(1.25);
    let mut rng = 0x9e3779b97f4a7c15u64;
    let mut best: Option<Circle> = None;
    let mut candidates = 0usize;
    for _ in 0..RANSAC_ITERATIONS {
        let i = next_random(&mut rng, points.len());
        let mut j = next_random(&mut rng, points.len());
        let mut k = next_random(&mut rng, points.len());
        if i == j { j = (j + 1) % points.len(); }
        if i == k || j == k { k = (k + 2) % points.len(); }
        let Some((x, y, radius)) = circle_from_three(points[i], points[j], points[k]) else { continue; };
        if !(min_radius..=max_radius).contains(&radius) || x < -radius || x > width as f64 + radius || y < -radius || y > height as f64 + radius { continue; }
        let mut bins = [false; ANGULAR_BINS];
        let mut residuals = Vec::new();
        for point in points {
            let residual = ((point.0-x).hypot(point.1-y) - radius).abs();
            if residual <= residual_limit {
                residuals.push(residual);
                let angle = (point.1-y).atan2(point.0-x).rem_euclid(TAU);
                bins[((angle / TAU) * ANGULAR_BINS as f64) as usize % ANGULAR_BINS] = true;
            }
        }
        let occupied = bins.iter().filter(|v| **v).count();
        if residuals.len() < MIN_INLIERS || occupied < MIN_ANGULAR_BINS { continue; }
        candidates += 1;
        residuals.sort_by(|a, b| a.total_cmp(b));
        let score = residuals.len() as f64 * (occupied as f64 / ANGULAR_BINS as f64).powi(2);
        let candidate = Circle { x, y, radius, inliers: residuals.len(), bins: occupied, residual: residuals[residuals.len()/2], score };
        if best.map(|current| score > current.score).unwrap_or(true) { best = Some(candidate); }
    }
    (best, candidates)
}

fn control_code(dx: f64, dy: f64) -> u32 {
    let horizontal = if dx >= 0.0 { 4 } else { 3 };
    let vertical = if dy >= 0.0 { 2 } else { 1 };
    if dx.abs() >= 4.0 && dy.abs() >= 4.0 && dx.abs() <= dy.abs()*3.0 && dy.abs() <= dx.abs()*3.0 {
        return match (vertical, horizontal) { (1,3)=>5, (1,4)=>6, (2,3)=>7, _=>8 };
    }
    if dx.abs() >= dy.abs() { horizontal } else { vertical }
}

#[no_mangle]
pub unsafe extern "system" fn elite_supercruise_sphere_analyze(
    pixels: *const u32,
    width: u32,
    height: u32,
    output: *mut SphereResult,
) -> i32 {
    if pixels.is_null() || output.is_null() || width == 0 || height == 0 || width as usize * height as usize > 65_536 { return -1; }
    let pixels = std::slice::from_raw_parts(pixels, width as usize * height as usize);
    let gray: Vec<u8> = pixels.iter().map(|p| luminance(*p)).collect();
    let blurred = blur_9x9(&gray, width as usize, height as usize);
    let threshold = otsu(&blurred);
    let binary: Vec<bool> = blurred.iter().map(|v| *v > threshold).collect();
    let white = binary.iter().filter(|v| **v).count();
    let points = boundary_points(&binary, width as usize, height as usize);
    let (circle, candidate_count) = fit_circle(&points, width as usize, height as usize);
    let mut result = SphereResult {
        state: 2,
        otsu_threshold: threshold as u32,
        black_permille: ((binary.len()-white) * 1000 / binary.len()) as u32,
        white_permille: (white * 1000 / binary.len()) as u32,
        boundary_point_count: points.len() as u32,
        candidate_count: candidate_count as u32,
        ..SphereResult::default()
    };
    if let Some(circle) = circle {
        let dx = width as f64 / 2.0 - circle.x;
        let dy = height as f64 / 2.0 - circle.y;
        let clearance = dx.hypot(dy) - circle.radius;
        let coverage = circle.bins as f64 / ANGULAR_BINS as f64;
        let inlier_ratio = circle.inliers as f64 / points.len().max(1) as f64;
        result.state = 1;
        result.control = control_code(dx, dy);
        result.center_x_milli = (circle.x * 1000.0).round() as i32;
        result.center_y_milli = (circle.y * 1000.0).round() as i32;
        result.radius_milli = (circle.radius * 1000.0).round() as u32;
        result.signed_clearance_milli = (clearance * 1000.0).round() as i32;
        result.confidence_permille = ((coverage * 650.0 + inlier_ratio * 350.0).min(1000.0)).round() as u32;
        result.occupied_angular_bins = circle.bins as u32;
        result.inlier_count = circle.inliers as u32;
        result.median_residual_milli = (circle.residual * 1000.0).round() as u32;
    }
    *output = result;
    0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_a_bright_sphere_and_points_away_from_its_center() {
        let (w, h) = (256usize, 144usize);
        let mut pixels = vec![0x030507u32; w*h];
        let (cx, cy, radius) = (164.0, 52.0, 26.0);
        for y in 0..h { for x in 0..w {
            if ((x as f64-cx).hypot(y as f64-cy)) <= radius { pixels[y*w+x] = 0xd86d86; }
        }}
        let mut out = SphereResult::default();
        let code = unsafe { elite_supercruise_sphere_analyze(pixels.as_ptr(), w as u32, h as u32, &mut out) };
        assert_eq!(code, 0);
        assert_eq!(out.state, 1);
        assert!(matches!(out.control, 5 | 7));
        assert!((out.center_x_milli - 164_000).abs() < 3_000);
        assert!((out.center_y_milli - 52_000).abs() < 3_000);
    }

    #[test]
    fn reports_absent_for_an_empty_starfield() {
        let pixels = vec![0x030507u32; 256*144];
        let mut out = SphereResult::default();
        let code = unsafe { elite_supercruise_sphere_analyze(pixels.as_ptr(), 256, 144, &mut out) };
        assert_eq!(code, 0);
        assert_eq!(out.state, 2);
    }
}
