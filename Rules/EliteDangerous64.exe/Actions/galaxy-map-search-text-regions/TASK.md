# Galaxy Map search text regions

Return raw PP-OCR text boxes from the fixed Galaxy Map title, search field, and
suggestion-list ROI. The owning workflow uses the title only to prove Galaxy
Map presence and accepts a suggestion only when its normalized text exactly
matches the complete requested System name.

This Action does not type, choose a fuzzy or partial result, click, plot a
route, interpret `NavRoute.json`, or close the map.
