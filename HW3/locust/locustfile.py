from locust.contrib.fasthttp import FastHttpUser
from locust import task, between
import random

class AlbumUser(FastHttpUser):
    wait_time = between(0.01, 0.05)  

    @task(3)
    def get_albums(self):
        self.client.get("/albums", name="GET /albums")

    @task(1)
    def post_album(self):
        album_id = random.randint(100000, 999999)
        payload = {
            "id": str(album_id),
            "title": f"Locust Album {album_id}",
            "artist": "Locust",
            "price": 9.99
        }
        self.client.post("/albums", json=payload, name="POST /albums")