# Navigation list text regions

Return raw PP-OCR text boxes from the fixed 1080p-reference Navigation list
ROI. The ROI is w480 and uses reference sampling. It intentionally includes
multiple visible rows so callers can locate the supplied target name and
preserve angle brackets as direct destination-lock evidence.

This finite observation Action does not choose a row, interpret lock state,
send input, or open and close the Navigation panel.
