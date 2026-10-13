#!/usr/bin/env python3
"""Makes the tables the query engine of examples/query is tested on, and
what SQLite answers to each query of queries/.

    python3 test/e2e/query/generate.py

The tables are the same at every run. The answers are written next to the
queries, as <name>.expected: they are what TestQueries compares the engine
with. SQLite (the sqlite3 command) is what decides what is right."""

import os, random, subprocess, sys

here = os.path.dirname(os.path.abspath(__file__))
data = os.path.join(here, "data")
queries = os.path.join(here, "queries")
random.seed(20261013)

cities = ["Lyon", "Nice", "Turin", "Oslo", "Accra", "Porto", "Ghent", "Split", "Tunis", "Quito"]
first = ["Ada", "Bao", "Chloe", "Dmitri", "Esi", "Farid", "Greta", "Hugo", "Ines", "Jonas", "Kofi", "Lena",
         "Mateo", "Nadia", "Omar", "Priya", "Quentin", "Rosa", "Sven", "Tara"]
last = ["Almeida", "Berg", "Chen", "Diallo", "Eriksen", "Ferrari", "Garcia", "Haddad", "Ito", "Jensen",
        "Kone", "Leroy", "Mensah", "Novak", "Okafor"]
tiers = ["bronze", "silver", "gold"]
categories = ["tools", "garden", "kitchen", "paper", "light"]
things = ["hammer", "rake", "kettle", "ledger", "lantern", "chisel", "trowel", "ladle", "notebook", "candle",
          "wrench", "hose", "whisk", "envelope", "bulb", "saw", "spade", "grater", "folder", "torch"]

tables = {}

rows = []
for i in range(1, 301):
    rows.append((i, random.choice(first) + " " + random.choice(last), random.choice(cities),
                 random.choice(tiers), 2010 + random.randrange(15)))
tables["customers"] = ("id:int,name:text,city:text,tier:text,joined:int", rows)

rows = []
for i in range(1, 61):
    name = things[(i - 1) % len(things)] + " " + "ABC"[(i - 1) // len(things)]
    rows.append((i, name, categories[(i * 7) % len(categories)], round(random.uniform(2, 180), 2)))
tables["products"] = ("id:int,name:text,category:text,price:real", rows)

rows = []
for i in range(1, 3001):
    # A few orders are of customers and products that are not there, for
    # the joins that keep rows without a match
    customer = random.randrange(1, 301) if i % 97 else 900 + i % 5
    product = random.randrange(1, 61) if i % 113 else 0
    rows.append((i, customer, product, random.randrange(1, 12),
                 random.choice([0.0, 0.0, 0.0, 0.05, 0.1, 0.25]), random.randrange(1, 366)))
tables["orders"] = ("id:int,customer_id:int,product_id:int,quantity:int,discount:real,day:int", rows)

kinds = {"int": "INTEGER", "real": "REAL", "text": "TEXT"}
setup = []
os.makedirs(data, exist_ok=True)
for name, (header, rows) in tables.items():
    with open(os.path.join(data, name + ".csv"), "w") as f:
        f.write(header + "\n")
        for row in rows:
            f.write(",".join(str(v) for v in row) + "\n")
    columns = [c.split(":") for c in header.split(",")]
    setup.append("CREATE TABLE %s (%s);" % (name, ", ".join("%s %s" % (c, kinds[k]) for c, k in columns)))
    for row in rows:
        values = ", ".join("'%s'" % v if isinstance(v, str) else repr(v) for v in row)
        setup.append("INSERT INTO %s VALUES (%s);" % (name, values))

database = os.path.join(here, ".oracle.db")
if os.path.exists(database):
    os.remove(database)
subprocess.run(["sqlite3", database], input="BEGIN;\n" + "\n".join(setup) + "\nCOMMIT;\n", text=True, check=True)

wrong = 0
for file in sorted(os.listdir(queries)):
    if not file.endswith(".sql"):
        continue
    sql = open(os.path.join(queries, file)).read()
    asked = subprocess.run(["sqlite3", "-separator", "|", "-nullvalue", "NULL", database],
                           input=sql, text=True, capture_output=True)
    if asked.returncode != 0 or asked.stderr:
        print("%s: SQLite refuses it: %s" % (file, asked.stderr.strip()))
        wrong += 1
        continue
    with open(os.path.join(queries, file[:-4] + ".expected"), "w") as f:
        f.write(asked.stdout)
os.remove(database)
sys.exit(1 if wrong else 0)
