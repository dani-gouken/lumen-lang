SELECT id, quantity * 3 + 1, quantity / 2, quantity % 4, -quantity, day - quantity * 10 FROM orders WHERE id <= 15 ORDER BY id;
