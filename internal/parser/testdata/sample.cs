using System;
using System.Collections.Generic;

namespace Server
{
    public struct Point
    {
        public double X;
        public double Y;
    }

    public interface IHandler
    {
        void Handle(string request);
    }

    public class Config
    {
        private string _host;
        private int _port;

        public Config(string host, int port)
        {
            _host = host;
            _port = port;
        }

        public string Address()
        {
            return $"{_host}:{_port}";
        }
    }

    public enum Status
    {
        Active,
        Inactive
    }
}
