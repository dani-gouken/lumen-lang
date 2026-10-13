SELECT orders.id, price > quantity, price = 0, quantity < 5.5, name < 'm', name > 5, quantity < 'a' FROM orders JOIN products ON products.id = product_id WHERE orders.id <= 10 ORDER BY orders.id;
