SELECT COUNT(*) FROM orders WHERE NULLIF(discount, 0.0) > 0.07;
