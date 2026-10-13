SELECT ROUND(AVG(quantity), 4), ROUND(AVG(discount), 5), ROUND(SUM(discount), 2), ROUND(MIN(price), 2) FROM orders JOIN products ON products.id = orders.product_id;
