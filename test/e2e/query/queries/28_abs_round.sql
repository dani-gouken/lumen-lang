SELECT orders.id, ABS(day - 180), ABS(discount - 0.1), ROUND(price), ROUND(price, 1), ROUND(quantity) FROM orders JOIN products ON products.id = product_id WHERE orders.id <= 15 ORDER BY orders.id;
