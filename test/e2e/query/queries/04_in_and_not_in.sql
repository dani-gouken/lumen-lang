SELECT id, city, tier FROM customers WHERE city IN ('Lyon', 'Nice', 'Accra') AND tier NOT IN ('bronze') AND id < 80 ORDER BY id;
