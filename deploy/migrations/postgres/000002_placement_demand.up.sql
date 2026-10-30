-- Portfolio-first: by default a placement shows only the owner's other apps.
-- Showing campaigns of other members (the invited circle) is opt-in.
CREATE TYPE demand_source AS ENUM ('portfolio', 'portfolio_and_circle');

ALTER TABLE placements
    ADD COLUMN demand demand_source NOT NULL DEFAULT 'portfolio';
