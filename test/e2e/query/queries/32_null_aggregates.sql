SELECT COUNT(*), COUNT(NULLIF(discount, 0.0)), ROUND(SUM(NULLIF(discount, 0.0)), 2), ROUND(AVG(NULLIF(discount, 0.0)), 4), MIN(NULLIF(quantity, 1)), MAX(NULLIF(quantity, 11)) FROM orders;
