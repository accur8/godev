#!/usr/bin/env fish


set ct 1
set max 999999999

while test $ct -le $max
  sleep 1 
  set message count = $ct -- (date)
  echo "{\"MESSAGE\":\"$message\"}"
  set ct (math $ct+1)
end

exit 10
